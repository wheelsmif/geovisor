package emitter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wheelsmif/geovisor/internal/tir"
)

func TestDefaultRegistry(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	want := []Format{FormatTIRJSON, FormatWebMCP, FormatMCP, FormatOpenAI}
	if got := registry.Formats(); !reflect.DeepEqual(got, want) {
		t.Fatalf("formats = %v, want %v", got, want)
	}
	_, err := registry.Emit(context.Background(), "missing", fixtureDocument(), Options{})
	if !IsErrorCode(err, CodeUnsupportedFormat) {
		t.Fatalf("error = %T %v, want unsupported format", err, err)
	}

	_, err = NewRegistry(MCP{}, MCP{})
	if !IsErrorCode(err, CodeRegistry) {
		t.Fatalf("duplicate registry error = %T %v", err, err)
	}
}

func TestCanonicalJSONDelegatesToTIR(t *testing.T) {
	t.Parallel()

	document := fixtureDocument()
	want, err := tir.Marshal(document)
	if err != nil {
		t.Fatalf("marshal TIR: %v", err)
	}
	result, err := (CanonicalJSON{}).Emit(context.Background(), document, Options{Strict: true})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !bytes.Equal(result.Primary.Data, want) {
		t.Fatal("canonical emitter differs from tir.Marshal")
	}
	if result.Companion != nil {
		t.Fatal("canonical TIR must not have a companion")
	}
}

func TestEmitterGoldensAndDeterminism(t *testing.T) {
	t.Parallel()

	document := fixtureDocument()
	before, err := tir.Marshal(document)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	tests := []struct {
		name    string
		emitter Emitter
		options Options
	}{
		{name: "tir", emitter: CanonicalJSON{}},
		{name: "webmcp", emitter: WebMCP{}},
		{name: "mcp", emitter: MCP{}},
		{name: "openai-nonstrict", emitter: OpenAI{}},
		{name: "openai-strict", emitter: OpenAI{}, options: Options{Strict: true}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			first, err := test.emitter.Emit(context.Background(), document, test.options)
			if err != nil {
				t.Fatalf("first emit: %v", err)
			}
			second, err := test.emitter.Emit(context.Background(), document, test.options)
			if err != nil {
				t.Fatalf("second emit: %v", err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatal("repeated output differs")
			}
			assertGolden(t, test.name+".golden", first.Primary.Data)
			if first.Companion != nil {
				assertGolden(t, test.name+"-bindings.golden", first.Companion.Data)
			}
		})
	}
	after, err := tir.Marshal(document)
	if err != nil {
		t.Fatalf("marshal fixture after emit: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("emission mutated caller-owned TIR")
	}
}

func TestOpenAIStrictOptionalityAndNestedObjects(t *testing.T) {
	t.Parallel()

	nonStrict := emitOpenAITools(t, false)
	strict := emitOpenAITools(t, true)
	nonStrictSchema := jsonObject(t, findOpenAITool(t, nonStrict, "complex_tool")["parameters"], "non-strict parameters")
	strictSchema := jsonObject(t, findOpenAITool(t, strict, "complex_tool")["parameters"], "strict parameters")

	assertStrings(t, nonStrictSchema["required"], []string{"query"})
	assertStrings(t, strictSchema["required"], []string{"query", "options"})

	nonStrictOptions := propertySchema(t, nonStrictSchema, "options")
	if nonStrictOptions["type"] != "object" {
		t.Fatalf("non-strict optional object type = %#v", nonStrictOptions["type"])
	}
	assertStrings(t, nonStrictOptions["required"], []string{"filters", "tags"})
	if propertySchema(t, nonStrictOptions, "count")["type"] != "integer" {
		t.Fatal("non-strict optional nested property must remain omittable and non-nullable")
	}
	strictOptions := propertySchema(t, strictSchema, "options")
	assertStrings(t, strictOptions["type"], []string{"object", "null"})
	assertStrings(t, strictOptions["required"], []string{"count", "filters", "tags"})
	if strictOptions["additionalProperties"] != false {
		t.Fatal("strict nested object must deny additional properties")
	}

	filters := propertySchema(t, strictOptions, "filters")
	assertStrings(t, filters["required"], []string{"label"})
	label := propertySchema(t, filters, "label")
	assertStrings(t, label["type"], []string{"string", "null"})
	labelEnum := jsonArray(t, label["enum"], "label.enum")
	if len(labelEnum) != 3 || labelEnum[2] != nil {
		t.Fatalf("strict optional enum = %#v, want trailing null", labelEnum)
	}

	tags := propertySchema(t, strictOptions, "tags")
	items := jsonObject(t, tags["items"], "tags.items")
	if items["additionalProperties"] != false {
		t.Fatal("strict array object item must deny additional properties")
	}
	assertStrings(t, items["required"], []string{"value"})
	assertStrings(t, propertySchema(t, items, "value")["type"], []string{"string", "null"})
}

func TestMCPConservativeAnnotations(t *testing.T) {
	t.Parallel()

	result, err := (MCP{}).Emit(context.Background(), fixtureDocument(), Options{})
	if err != nil {
		t.Fatalf("emit MCP: %v", err)
	}
	var manifest struct {
		Tools []struct {
			Name        string         `json:"name"`
			Annotations mcpAnnotations `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result.Primary.Data, &manifest); err != nil {
		t.Fatalf("decode MCP: %v", err)
	}
	if len(manifest.Tools) != 2 {
		t.Fatalf("tool count = %d", len(manifest.Tools))
	}
	if got := manifest.Tools[0].Annotations; got != (mcpAnnotations{
		ReadOnlyHint: false, DestructiveHint: true, IdempotentHint: false, OpenWorldHint: true,
	}) {
		t.Fatalf("complex annotations = %#v", got)
	}
	if got := manifest.Tools[1].Annotations; got != (mcpAnnotations{
		ReadOnlyHint: true, DestructiveHint: false, IdempotentHint: true, OpenWorldHint: false,
	}) {
		t.Fatalf("read-only annotations = %#v", got)
	}
}

func TestAnnotationDerivationForEverySideEffect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sideEffect    tir.SideEffectClass
		readOnly      bool
		openWorld     bool
		consequential bool
	}{
		{sideEffect: tir.SideEffectNone, readOnly: true},
		{sideEffect: tir.SideEffectLocalState},
		{sideEffect: tir.SideEffectNetwork, openWorld: true, consequential: true},
		{sideEffect: tir.SideEffectNavigation, openWorld: true, consequential: true},
		{sideEffect: tir.SideEffectSubmission, openWorld: true, consequential: true},
		{sideEffect: tir.SideEffectUnknown, openWorld: true, consequential: true},
	}
	for _, test := range tests {
		test := test
		t.Run(string(test.sideEffect), func(t *testing.T) {
			t.Parallel()
			document := annotationDocument(test.sideEffect)
			mcpResult, err := (MCP{}).Emit(context.Background(), document, Options{})
			if err != nil {
				t.Fatalf("emit MCP: %v", err)
			}
			var manifest struct {
				Tools []struct {
					Annotations mcpAnnotations `json:"annotations"`
				} `json:"tools"`
			}
			if err := json.Unmarshal(mcpResult.Primary.Data, &manifest); err != nil {
				t.Fatalf("decode MCP: %v", err)
			}
			if len(manifest.Tools) != 1 {
				t.Fatalf("MCP tool count = %d", len(manifest.Tools))
			}
			mcp := manifest.Tools[0].Annotations
			if mcp.ReadOnlyHint != test.readOnly ||
				mcp.DestructiveHint != !test.readOnly ||
				mcp.IdempotentHint != test.readOnly ||
				mcp.OpenWorldHint != test.openWorld {
				t.Fatalf("MCP annotations = %#v", mcp)
			}

			webResult, err := (WebMCP{}).Emit(context.Background(), document, Options{})
			if err != nil {
				t.Fatalf("emit WebMCP: %v", err)
			}
			definitions := decodeWebMCPDefinitions(t, webResult.Primary.Data)
			if len(definitions) != 1 {
				t.Fatalf("WebMCP tool count = %d", len(definitions))
			}
			web := jsonObject(t, definitions[0]["annotations"], "annotations")
			if web["readOnlyHint"] != test.readOnly ||
				web["consequentialHint"] != test.consequential ||
				web["untrustedContentHint"] != true {
				t.Fatalf("WebMCP annotations = %#v", web)
			}
		})
	}
}

// TestWebMCPModuleSafetyInvariants checks the properties of the emitted text:
// escaping, the capability guard, and how the embedded runtime is wired to the
// definitions. It deliberately does not claim to test runtime behavior --
// substring searches cannot tell a working module from a file containing the
// right words (GV-038). Runtime behavior is covered by executing the module in
// internal/integration/webmcp_roundtrip_test.go (GV-036).
func TestWebMCPModuleSafetyInvariants(t *testing.T) {
	t.Parallel()

	result, err := (WebMCP{}).Emit(context.Background(), fixtureDocument(), Options{})
	if err != nil {
		t.Fatalf("emit WebMCP: %v", err)
	}
	source := string(result.Primary.Data)
	for _, required := range []string{
		"document.modelContext.registerTool",
		"__geovisorRuntime.register(__geovisorDefinitions)",
		"page JavaScript cannot bypass this browser boundary",
		"shadowRoot",
		"consequentialHint",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("module does not contain %q", required)
		}
	}
	if strings.Contains(strings.ToLower(source), "</script>") {
		t.Fatal("module contains an unescaped script end tag")
	}
	if strings.ContainsRune(source, '\u2028') || strings.ContainsRune(source, '\u2029') {
		t.Fatal("module contains a raw JavaScript line separator")
	}
	if strings.Contains(source, "playwright") {
		t.Fatal("module must not depend on Playwright")
	}
}

func TestCompanionBindingsAreSeparateAndResolved(t *testing.T) {
	t.Parallel()

	document := fixtureDocument()
	for _, target := range []Format{FormatMCP, FormatOpenAI} {
		target := target
		t.Run(string(target), func(t *testing.T) {
			t.Parallel()
			artifact, err := BindingArtifact(context.Background(), document, target)
			if err != nil {
				t.Fatalf("emit bindings: %v", err)
			}
			var decoded bindingArtifact
			if err := json.Unmarshal(artifact.Data, &decoded); err != nil {
				t.Fatalf("decode bindings: %v", err)
			}
			if decoded.SchemaVersion != bindingSchemaVersion || decoded.Target != target {
				t.Fatalf("binding header = %#v", decoded)
			}
			if len(decoded.Tools) != 2 || len(decoded.Tools[0].Actions) != 2 {
				t.Fatalf("unexpected recipes: %#v", decoded.Tools)
			}
			for _, action := range decoded.Tools[0].Actions {
				if len(action.Locators) != 1 || action.Locators[0].ID == "" {
					t.Fatalf("unresolved action recipe: %#v", action)
				}
			}
		})
	}
	_, err := BindingArtifact(context.Background(), document, FormatWebMCP)
	if !IsErrorCode(err, CodeUnsupportedFormat) {
		t.Fatalf("WebMCP binding error = %T %v", err, err)
	}
}

func TestTypedErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		emitter Emitter
		mutate  func(*tir.Document)
		code    ErrorCode
	}{
		{
			name:    "array missing items",
			emitter: MCP{},
			mutate: func(document *tir.Document) {
				tool := fixtureTool(document, "complex_tool")
				tool.Parameters[0].Type = tir.ValueArray
				tool.Parameters[0].Enum = []string{}
			},
			code: CodeInvalidDocument,
		},
		{
			name:    "primitive properties",
			emitter: OpenAI{},
			mutate: func(document *tir.Document) {
				tool := fixtureTool(document, "complex_tool")
				tool.Parameters[0].Properties = []tir.ParameterProperty{{
					Name: "bad", Shape: tir.ParameterShape{Type: tir.ValueString},
				}}
			},
			code: CodeInvalidDocument,
		},
		{
			name:    "duplicate nested property",
			emitter: MCP{},
			mutate: func(document *tir.Document) {
				tool := fixtureTool(document, "complex_tool")
				properties := tool.Parameters[1].Properties
				tool.Parameters[1].Properties = append(properties, properties[0])
			},
			code: CodeInvalidDocument,
		},
		{
			name:    "malformed locator reference",
			emitter: OpenAI{},
			mutate: func(document *tir.Document) {
				document.Tools[0].Actions[0].LocatorCandidateIDs[0] = "missing"
			},
			code: CodeInvalidDocument,
		},
		{
			name:    "invalid tool id",
			emitter: OpenAI{},
			mutate: func(document *tir.Document) {
				document.Tools[0].ID = "invalid name"
			},
			code: CodeInvalidDocument,
		},
		{
			name:    "WebMCP action without locator",
			emitter: WebMCP{},
			mutate: func(document *tir.Document) {
				document.Tools[0].Actions[0].LocatorCandidateIDs = []string{}
			},
			code: CodeUnsupportedShape,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document := fixtureDocument()
			test.mutate(document)
			_, err := test.emitter.Emit(context.Background(), document, Options{Strict: true})
			if !IsErrorCode(err, test.code) {
				t.Fatalf("error = %T %v, want %s", err, err, test.code)
			}
		})
	}
}

func TestValidDocumentEmitsOnEveryFormat(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, format := range registry.Formats() {
		if _, err := registry.Emit(context.Background(), format, fixtureDocument(), Options{Strict: true}); err != nil {
			t.Fatalf("emit %s: %v", format, err)
		}
	}
}

func TestMCPAndWebMCPHonorStrict(t *testing.T) {
	t.Parallel()

	nonStrictMCP := emitMCPTools(t, false)
	strictMCP := emitMCPTools(t, true)
	nonStrictSchema := jsonObject(t, findMCPTool(t, nonStrictMCP, "complex_tool")["inputSchema"], "non-strict MCP schema")
	strictSchema := jsonObject(t, findMCPTool(t, strictMCP, "complex_tool")["inputSchema"], "strict MCP schema")
	assertStrings(t, nonStrictSchema["required"], []string{"query"})
	assertStrings(t, strictSchema["required"], []string{"query", "options"})

	nonStrictWebMCP := emitWebMCPDefinitions(t, false)
	strictWebMCP := emitWebMCPDefinitions(t, true)
	assertStrings(t, jsonObject(t, findWebMCPTool(t, nonStrictWebMCP, "complex_tool")["inputSchema"], "non-strict WebMCP schema")["required"], []string{"query"})
	assertStrings(t, jsonObject(t, findWebMCPTool(t, strictWebMCP, "complex_tool")["inputSchema"], "strict WebMCP schema")["required"], []string{"query", "options"})
}

func TestCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, candidate := range []Emitter{CanonicalJSON{}, WebMCP{}, MCP{}, OpenAI{}} {
		_, err := candidate.Emit(ctx, fixtureDocument(), Options{})
		if !IsErrorCode(err, CodeCanceled) || !errors.Is(err, context.Canceled) {
			t.Fatalf("%s error = %T %v", candidate.Format(), err, err)
		}
	}
}

func TestEmitterSchemasUseJSONSchema202012(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"emitter-bindings.schema.json",
		"mcp-tools-list-2026-07-28.schema.json",
		"openai-function-tools.schema.json",
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", "schemas", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var schema map[string]any
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if schema["$schema"] != JSONSchemaDialect {
			t.Fatalf("%s dialect = %#v", name, schema["$schema"])
		}
	}
}

func emitMCPTools(t *testing.T, strict bool) []map[string]any {
	t.Helper()
	result, err := (MCP{}).Emit(context.Background(), fixtureDocument(), Options{Strict: strict})
	if err != nil {
		t.Fatalf("emit MCP: %v", err)
	}
	var manifest struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(result.Primary.Data, &manifest); err != nil {
		t.Fatalf("decode MCP: %v", err)
	}
	return manifest.Tools
}

func findMCPTool(t *testing.T, tools []map[string]any, name string) map[string]any {
	t.Helper()
	for _, tool := range tools {
		if tool["name"] == name {
			return tool
		}
	}
	t.Fatalf("MCP tool %q not found", name)
	return nil
}

func emitWebMCPDefinitions(t *testing.T, strict bool) []map[string]any {
	t.Helper()
	result, err := (WebMCP{}).Emit(context.Background(), fixtureDocument(), Options{Strict: strict})
	if err != nil {
		t.Fatalf("emit WebMCP: %v", err)
	}
	return decodeWebMCPDefinitions(t, result.Primary.Data)
}

func decodeWebMCPDefinitions(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	prefix := []byte("const __geovisorDefinitions = ")
	start := bytes.Index(data, prefix)
	if start < 0 {
		t.Fatal("WebMCP module is missing definitions")
	}
	start += len(prefix)
	end := bytes.Index(data[start:], []byte(";\n"))
	if end < 0 {
		t.Fatal("WebMCP definitions are not terminated")
	}
	var definitions []map[string]any
	if err := json.Unmarshal(data[start:start+end], &definitions); err != nil {
		t.Fatalf("decode WebMCP definitions: %v", err)
	}
	return definitions
}

func annotationDocument(class tir.SideEffectClass) *tir.Document {
	document := tir.NewDocument(tir.SourceMetadata{
		Kind:              tir.SourceLaunchURL,
		ExecutionBoundary: tir.ExecutionAgentOwned,
	})
	document.FrameCoverage.Status = tir.CoverageComplete
	document.FrameCoverage.Frames = []tir.CoveredFrame{{Path: []tir.FrameReference{}}}
	safe := class != tir.SideEffectNavigation &&
		class != tir.SideEffectSubmission &&
		class != tir.SideEffectUnknown
	document.Tools = []tir.Tool{{
		ID:   "hint_tool",
		Name: "Hint",
		Locators: []tir.LocatorCandidate{{
			ID:          "hint-locator",
			CSSFallback: "#hint",
			Confidence:  tir.Confidence{Score: 1},
		}},
		Actions: []tir.ActionBinding{{
			Action:              tir.ActionClick,
			LocatorCandidateIDs: []string{"hint-locator"},
			SideEffect:          tir.SideEffect{Class: class, SafeForExploration: safe},
		}},
		Confidence: tir.Confidence{Score: 1},
	}}
	document.Normalize()
	return document
}

func jsonObject(t *testing.T, value any, path string) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want object", path, value)
	}
	return object
}

func jsonArray(t *testing.T, value any, path string) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("%s = %T, want array", path, value)
	}
	return array
}

func findWebMCPTool(t *testing.T, tools []map[string]any, name string) map[string]any {
	t.Helper()
	for _, tool := range tools {
		if tool["name"] == name {
			return tool
		}
	}
	t.Fatalf("WebMCP tool %q not found", name)
	return nil
}

func emitOpenAITools(t *testing.T, strict bool) []map[string]any {
	t.Helper()
	result, err := (OpenAI{}).Emit(context.Background(), fixtureDocument(), Options{Strict: strict})
	if err != nil {
		t.Fatalf("emit OpenAI: %v", err)
	}
	var tools []map[string]any
	if err := json.Unmarshal(result.Primary.Data, &tools); err != nil {
		t.Fatalf("decode OpenAI: %v", err)
	}
	return tools
}

func findOpenAITool(t *testing.T, tools []map[string]any, name string) map[string]any {
	t.Helper()
	for _, tool := range tools {
		if tool["name"] == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func propertySchema(t *testing.T, schema map[string]any, name string) map[string]any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %T", schema["properties"])
	}
	property, ok := properties[name].(map[string]any)
	if !ok {
		t.Fatalf("property %q = %T", name, properties[name])
	}
	return property
}

func assertStrings(t *testing.T, actual any, want []string) {
	t.Helper()
	values, ok := actual.([]any)
	if !ok {
		t.Fatalf("value = %T %#v, want string array", actual, actual)
	}
	got := make([]string, len(values))
	for i, value := range values {
		got[i], ok = value.(string)
		if !ok {
			t.Fatalf("value[%d] = %T", i, value)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("value = %v, want %v", got, want)
	}
}

func assertGolden(t *testing.T, name string, actual []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, actual, 0o644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if !bytes.Equal(actual, want) {
		t.Fatalf("%s differs from golden", name)
	}
}

func fixtureTool(document *tir.Document, id string) *tir.Tool {
	for i := range document.Tools {
		if document.Tools[i].ID == id {
			return &document.Tools[i]
		}
	}
	panic("fixture tool not found: " + id)
}

func fixtureDocument() *tir.Document {
	document := tir.NewDocument(tir.SourceMetadata{
		Kind:              tir.SourceLaunchURL,
		ExecutionBoundary: tir.ExecutionAgentOwned,
		RequestedURL:      "https://example.test/",
		FinalURL:          "https://example.test/app",
	})
	document.FrameCoverage.Status = tir.CoverageComplete
	document.FrameCoverage.Frames = []tir.CoveredFrame{{Path: []tir.FrameReference{}}}
	document.Tools = []tir.Tool{
		{
			ID:          "read_only",
			Name:        "Read only",
			Description: "Inspect a local control.",
			Locators: []tir.LocatorCandidate{{
				ID:          "read-control",
				FramePath:   []tir.PathNode{},
				ShadowPath:  []tir.PathNode{},
				CSSFallback: "#read-control",
				Confidence:  tir.Confidence{Score: 1},
				Provenance:  []tir.Provenance{{Kind: tir.ProvenanceDOM}},
			}},
			Actions: []tir.ActionBinding{{
				Action:              tir.ActionClick,
				LocatorCandidateIDs: []string{"read-control"},
				SideEffect:          tir.SideEffect{Class: tir.SideEffectNone, SafeForExploration: true},
			}},
			Confidence: tir.Confidence{Score: 1},
			Provenance: []tir.Provenance{{Kind: tir.ProvenanceDOM}},
		},
		{
			ID:          "complex_tool",
			Name:        "Complex </script> \u2028 \u2029",
			Description: "Submit </script> safely \u2028 without embedding current values.",
			Parameters: []tir.Parameter{
				{
					Name:        "query",
					Description: "Search text </script>",
					Type:        tir.ValueString,
					Required:    true,
					Enum:        []string{"alpha", "</script>"},
				},
				{
					Name:        "options",
					Description: "Optional nested controls",
					Type:        tir.ValueObject,
					Properties: []tir.ParameterProperty{
						{
							Name:        "count",
							Description: "Optional count",
							Shape:       tir.ParameterShape{Type: tir.ValueInteger},
						},
						{
							Name:     "filters",
							Required: true,
							Shape: tir.ParameterShape{
								Type: tir.ValueObject,
								Properties: []tir.ParameterProperty{{
									Name: "label",
									Shape: tir.ParameterShape{
										Type: tir.ValueString,
										Enum: []string{"safe", "unsafe"},
									},
								}},
							},
						},
						{
							Name:     "tags",
							Required: true,
							Shape: tir.ParameterShape{
								Type: tir.ValueArray,
								Items: &tir.ParameterShape{
									Type: tir.ValueObject,
									Properties: []tir.ParameterProperty{{
										Name:  "value",
										Shape: tir.ParameterShape{Type: tir.ValueString},
									}},
								},
							},
						},
					},
				},
			},
			Locators: []tir.LocatorCandidate{
				{
					ID: "submit-button",
					FramePath: []tir.PathNode{{
						Semantic:    &tir.SemanticNode{Role: "iframe", Name: "Checkout"},
						CSSFallback: "iframe.checkout",
					}},
					ShadowPath: []tir.PathNode{{
						CSSFallback: "checkout-form",
					}},
					Semantic: &tir.SemanticLocator{
						Scope: []tir.SemanticNode{{Role: "form", Name: "Search"}},
						Role:  "button",
						Name:  "Submit </script>",
					},
					CSSFallback: "button[type=\"submit\"]",
					Confidence:  tir.Confidence{Score: 0.8},
					Provenance:  []tir.Provenance{{Kind: tir.ProvenanceAccessibility}},
				},
				{
					ID:         "query-input",
					FramePath:  []tir.PathNode{},
					ShadowPath: []tir.PathNode{},
					Semantic: &tir.SemanticLocator{
						Scope: []tir.SemanticNode{},
						Role:  "searchbox",
						Name:  "Query",
					},
					CSSFallback: "#query",
					Confidence:  tir.Confidence{Score: 0.95},
					Provenance:  []tir.Provenance{{Kind: tir.ProvenanceAccessibility}},
				},
			},
			Actions: []tir.ActionBinding{
				{
					Action:              tir.ActionFill,
					InputParameter:      "query",
					LocatorCandidateIDs: []string{"query-input"},
					SideEffect:          tir.SideEffect{Class: tir.SideEffectNone, SafeForExploration: true},
				},
				{
					Action:              tir.ActionClick,
					LocatorCandidateIDs: []string{"submit-button"},
					SideEffect: tir.SideEffect{
						Class:              tir.SideEffectSubmission,
						SafeForExploration: false,
						Rationale:          "Submits the form.",
					},
				},
			},
			Confidence: tir.Confidence{Score: 0.85},
			Provenance: []tir.Provenance{{Kind: tir.ProvenanceAccessibility}},
		},
	}
	document.Normalize()
	return document
}

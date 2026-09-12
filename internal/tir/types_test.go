package tir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewDocumentMarshalsCollectionsAsArrays(t *testing.T) {
	t.Parallel()

	document := NewDocument(SourceMetadata{
		Kind:              SourceLaunchURL,
		ExecutionBoundary: ExecutionAgentOwned,
	})
	document.Tools = append(document.Tools, Tool{
		ID:   "search",
		Name: "Search",
		Parameters: []Parameter{{
			Name: "query",
			Type: ValueString,
		}},
		Locators: []LocatorCandidate{{
			ID: "query-input",
			Semantic: &SemanticLocator{
				Role: "searchbox",
			},
		}},
		Actions: []ActionBinding{{
			Action: ActionFill,
			SideEffect: SideEffect{
				Class:              SideEffectNone,
				SafeForExploration: true,
			},
		}},
	})
	document.Normalize()

	first, err := Marshal(document)
	if err != nil {
		t.Fatalf("marshal TIR: %v", err)
	}
	second, err := Marshal(document)
	if err != nil {
		t.Fatalf("marshal TIR again: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("marshaling the same TIR was not deterministic")
	}

	var decoded map[string]any
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("decode TIR: %v", err)
	}
	assertJSONArray(t, decoded, "tools")
	assertJSONArray(t, decoded, "warnings")

	frameCoverage := jsonObject(t, decoded["frameCoverage"], "frameCoverage")
	assertJSONArray(t, frameCoverage, "frames")
	assertJSONArray(t, frameCoverage, "uncovered")

	tools := jsonArray(t, decoded["tools"], "tools")
	if len(tools) == 0 {
		t.Fatal("tools must not be empty")
	}
	tool := jsonObject(t, tools[0], "tools[0]")
	assertJSONArray(t, tool, "parameters")
	assertJSONArray(t, tool, "locatorCandidates")
	assertJSONArray(t, tool, "actionBindings")
	assertJSONArray(t, tool, "provenance")

	parameters := jsonArray(t, tool["parameters"], "tools[0].parameters")
	if len(parameters) == 0 {
		t.Fatal("parameters must not be empty")
	}
	parameter := jsonObject(t, parameters[0], "tools[0].parameters[0]")
	assertJSONArray(t, parameter, "enum")
	assertJSONArray(t, parameter, "properties")
}

func TestValidateRejectsUnsafeExploration(t *testing.T) {
	t.Parallel()

	for _, class := range []SideEffectClass{SideEffectNavigation, SideEffectSubmission, SideEffectUnknown} {
		class := class
		t.Run(string(class), func(t *testing.T) {
			t.Parallel()
			document := NewDocument(SourceMetadata{
				Kind:              SourceLaunchURL,
				ExecutionBoundary: ExecutionAgentOwned,
			})
			document.Tools = append(document.Tools, Tool{
				Actions: []ActionBinding{{
					Action: ActionClick,
					SideEffect: SideEffect{
						Class:              class,
						SafeForExploration: true,
					},
				}},
			})
			err := document.Validate()
			if !IsValidationError(err) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
		})
	}
}

func TestSchemaVersionMatchesGoContract(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test file")
	}
	schemaPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "schemas", "tir.schema.json")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}

	var schema struct {
		Schema     string `json:"$schema"`
		Properties struct {
			SchemaVersion struct {
				Const string `json:"const"`
			} `json:"schemaVersion"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	if schema.Schema != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("unexpected JSON Schema dialect %q", schema.Schema)
	}
	if schema.Properties.SchemaVersion.Const != SchemaVersion {
		t.Fatalf(
			"schema version %q does not match Go contract %q",
			schema.Properties.SchemaVersion.Const,
			SchemaVersion,
		)
	}
}

func assertJSONArray(t *testing.T, object map[string]any, key string) {
	t.Helper()
	jsonArray(t, object[key], key)
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

package emitter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wheelsmif/geovisor/internal/tir"
)

func TestDTOJSONTagsMatchSchemas(t *testing.T) {
	t.Parallel()
	root := schemaObject(t, "tir.schema.json")
	defs := objectField(t, root, "$defs")
	assertStructSchema(t, tir.Document{}, objectField(t, root, "properties"), stringList(t, root["required"]))
	pairs := []struct {
		value reflect.Type
		def   string
	}{
		{reflect.TypeOf(tir.SourceMetadata{}), "sourceMetadata"},
		{reflect.TypeOf(tir.FrameCoverage{}), "frameCoverage"},
		{reflect.TypeOf(tir.CoveredFrame{}), "coveredFrame"},
		{reflect.TypeOf(tir.UncoveredFrame{}), "uncoveredFrame"},
		{reflect.TypeOf(tir.FrameReference{}), "frameReference"},
		{reflect.TypeOf(tir.Tool{}), "tool"},
		{reflect.TypeOf(tir.Parameter{}), "parameter"},
		{reflect.TypeOf(tir.ParameterShape{}), "parameterShape"},
		{reflect.TypeOf(tir.ParameterProperty{}), "parameterProperty"},
		{reflect.TypeOf(tir.LocatorCandidate{}), "locatorCandidate"},
		{reflect.TypeOf(tir.PathNode{}), "pathNode"},
		{reflect.TypeOf(tir.SemanticLocator{}), "semanticLocator"},
		{reflect.TypeOf(tir.SemanticNode{}), "semanticNode"},
		{reflect.TypeOf(tir.ActionBinding{}), "actionBinding"},
		{reflect.TypeOf(tir.SideEffect{}), "sideEffect"},
		{reflect.TypeOf(tir.Confidence{}), "confidence"},
		{reflect.TypeOf(tir.Provenance{}), "provenance"},
		{reflect.TypeOf(tir.Warning{}), "warning"},
	}
	for _, pair := range pairs {
		definition := objectField(t, defs, pair.def)
		assertStructSchema(t, reflect.New(pair.value).Elem().Interface(), objectField(t, definition, "properties"), stringList(t, definition["required"]))
	}

	bindings := schemaObject(t, "emitter-bindings.schema.json")
	assertStructSchema(t, bindingArtifact{}, objectField(t, bindings, "properties"), stringList(t, bindings["required"]))
	toolSchema := objectField(t, objectField(t, bindings, "properties"), "tools")
	toolItem := objectField(t, toolSchema, "items")
	assertStructSchema(t, toolBindingRecipe{}, objectField(t, toolItem, "properties"), stringList(t, toolItem["required"]))
	actionSchema := objectField(t, objectField(t, toolItem, "properties"), "actions")
	actionItem := objectField(t, actionSchema, "items")
	assertStructSchema(t, actionBindingRecipe{}, objectField(t, actionItem, "properties"), stringList(t, actionItem["required"]))
}

func TestEmitterGoldensMatchSchemas(t *testing.T) {
	t.Parallel()
	cases := []struct {
		golden string
		schema string
	}{
		{"tir.golden", "tir.schema.json"},
		{"mcp.golden", "mcp-tools-list-" + MCPSpecVersion + ".schema.json"},
		{"mcp-bindings.golden", "emitter-bindings.schema.json"},
		{"openai-nonstrict.golden", "openai-function-tools.schema.json"},
		{"openai-nonstrict-bindings.golden", "emitter-bindings.schema.json"},
		{"openai-strict.golden", "openai-function-tools.schema.json"},
		{"openai-strict-bindings.golden", "emitter-bindings.schema.json"},
	}
	for _, test := range cases {
		payload, err := os.ReadFile(filepath.Join("testdata", test.golden))
		if err != nil {
			t.Fatalf("read %s: %v", test.golden, err)
		}
		validatePayload(t, test.schema, payload)
	}
}

func assertStructSchema(t *testing.T, value any, properties map[string]any, required []string) {
	t.Helper()
	tags := jsonTags(t, reflect.TypeOf(value))
	for name := range properties {
		tag, ok := tags[name]
		if !ok {
			t.Fatalf("%T schema property %q has no json tag", value, name)
		}
		if containsString(required, name) && strings.Contains(tag, "omitempty") {
			t.Fatalf("%T required field %q is omitempty", value, name)
		}
	}
	for name := range tags {
		if _, ok := properties[name]; !ok {
			t.Fatalf("%T json tag %q is not a schema property", value, name)
		}
	}
}

func jsonTags(t *testing.T, typ reflect.Type) map[string]string {
	t.Helper()
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	tags := make(map[string]string)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		tags[name] = tag
	}
	return tags
}

func validatePayload(t *testing.T, schemaName string, payload []byte) {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	for _, name := range []string{
		"tir.schema.json",
		"mcp-tools-list-" + MCPSpecVersion + ".schema.json",
		"openai-function-tools.schema.json",
		"emitter-bindings.schema.json",
	} {
		resource := schemaObject(t, name)
		id, _ := resource["$id"].(string)
		if err := compiler.AddResource(id, resource); err != nil {
			t.Fatalf("add schema %s: %v", name, err)
		}
	}
	schema, err := compiler.Compile(fmt.Sprintf("https://wheelsmif.github.io/geovisor/schemas/%s", schemaName))
	if err != nil {
		t.Fatalf("compile %s: %v", schemaName, err)
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatalf("decode %s payload: %v", schemaName, err)
	}
	if err := schema.Validate(value); err != nil {
		t.Fatalf("%s: %v", schemaName, err)
	}
}

func schemaObject(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", name))
	if err != nil {
		t.Fatalf("read schema %s: %v", name, err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode schema %s: %v", name, err)
	}
	return value
}

func objectField(t *testing.T, parent map[string]any, name string) map[string]any {
	t.Helper()
	value, ok := parent[name].(map[string]any)
	if !ok {
		t.Fatalf("%q is %T, want object", name, parent[name])
	}
	return value
}

func stringList(t *testing.T, value any) []string {
	t.Helper()
	if value == nil {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("required is %T, want array", value)
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("required item is %T", item)
		}
		result = append(result, text)
	}
	return result
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

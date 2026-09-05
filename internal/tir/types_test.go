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

	first, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal TIR: %v", err)
	}
	second, err := json.Marshal(document)
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

	frameCoverage := decoded["frameCoverage"].(map[string]any)
	assertJSONArray(t, frameCoverage, "frames")
	assertJSONArray(t, frameCoverage, "uncovered")

	tool := decoded["tools"].([]any)[0].(map[string]any)
	assertJSONArray(t, tool, "parameters")
	assertJSONArray(t, tool, "locatorCandidates")
	assertJSONArray(t, tool, "actionBindings")
	assertJSONArray(t, tool, "provenance")

	parameter := tool["parameters"].([]any)[0].(map[string]any)
	assertJSONArray(t, parameter, "enum")
	assertJSONArray(t, parameter, "properties")
}

func TestValidateRejectsUnsafeExploration(t *testing.T) {
	t.Parallel()

	document := NewDocument(SourceMetadata{
		Kind:              SourceLaunchURL,
		ExecutionBoundary: ExecutionAgentOwned,
	})
	document.Tools = append(document.Tools, Tool{
		Actions: []ActionBinding{{
			Action: ActionClick,
			SideEffect: SideEffect{
				Class:              SideEffectSubmission,
				SafeForExploration: true,
			},
		}},
	})

	err := document.Validate()
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
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
	if _, ok := object[key].([]any); !ok {
		t.Fatalf("%q must be a non-null JSON array, got %T", key, object[key])
	}
}

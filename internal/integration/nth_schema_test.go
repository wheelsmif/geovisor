package integration

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wheelsmif/geovisor/internal/tir"
)

func TestNegativeNthFailsValidateAndSchema(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	schema := loadTIRSchema(t, root)

	tests := []struct {
		name   string
		mutate func(*tir.Document)
	}{
		{
			name: "semantic locator nth",
			mutate: func(document *tir.Document) {
				document.Tools[0].Locators[0].Semantic.Nth = -1
			},
		},
		{
			name: "semantic scope nth",
			mutate: func(document *tir.Document) {
				document.Tools[0].Locators[0].Semantic.Scope = []tir.SemanticNode{{
					Role: "region", Name: "Panel", Nth: -1,
				}}
			},
		},
		{
			name: "path node nth",
			mutate: func(document *tir.Document) {
				document.Tools[0].Locators[0].ShadowPath = []tir.PathNode{{
					Semantic: &tir.SemanticNode{Role: "generic", Nth: -1},
				}}
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document := validNthDocument()
			test.mutate(document)
			if err := document.Validate(); err == nil {
				t.Fatal("Validate accepted a negative nth")
			}
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(value); err == nil {
				t.Fatal("JSON Schema accepted a negative nth")
			}
		})
	}

	valid := validNthDocument()
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid document failed Validate: %v", err)
	}
}

func validNthDocument() *tir.Document {
	document := tir.NewDocument(tir.SourceMetadata{
		Kind:              tir.SourceLaunchURL,
		ExecutionBoundary: tir.ExecutionAgentOwned,
	})
	document.FrameCoverage.Status = tir.CoverageComplete
	document.FrameCoverage.Frames = []tir.CoveredFrame{{Path: []tir.FrameReference{}}}
	document.Tools = []tir.Tool{{
		ID:   "query_tool",
		Name: "Query",
		Locators: []tir.LocatorCandidate{{
			ID: "query-locator",
			Semantic: &tir.SemanticLocator{
				Scope: []tir.SemanticNode{},
				Role:  "textbox",
				Name:  "Query",
				Nth:   0,
			},
			Confidence: tir.Confidence{Score: 1},
		}},
		Actions: []tir.ActionBinding{{
			Action:              tir.ActionFill,
			LocatorCandidateIDs: []string{"query-locator"},
			SideEffect:          tir.SideEffect{Class: tir.SideEffectUnknown},
		}},
		Confidence: tir.Confidence{Score: 1},
	}}
	document.Normalize()
	return document
}

func loadTIRSchema(t *testing.T, root string) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	path := filepath.Join(root, "schemas", "tir.schema.json")
	var resource any
	decodeFile(t, path, &resource)
	resourceObject, ok := resource.(map[string]any)
	if !ok {
		t.Fatalf("schema is %T", resource)
	}
	id, ok := resourceObject["$id"].(string)
	if !ok {
		t.Fatal("schema $id missing")
	}
	if err := compiler.AddResource(id, resource); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(id)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

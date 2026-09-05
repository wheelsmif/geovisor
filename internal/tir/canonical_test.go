package tir

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestMarshalCanonicalizesWithoutMutatingCaller(t *testing.T) {
	t.Parallel()

	document := validDocument()
	document.Tools[0].Parameters[0].Enum = []string{"z", "a", "z"}
	original := append([]string(nil), document.Tools[0].Parameters[0].Enum...)

	first, err := Marshal(document)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	second, err := Marshal(document)
	if err != nil {
		t.Fatalf("marshal again: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("canonical bytes differ")
	}
	if !equalStrings(document.Tools[0].Parameters[0].Enum, original) {
		t.Fatalf("Marshal mutated caller enum: %v", document.Tools[0].Parameters[0].Enum)
	}
	if len(first) > 0 && first[len(first)-1] == '\n' {
		t.Fatal("Marshal must not append a newline")
	}

	var output bytes.Buffer
	if err := Write(&output, document); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !bytes.Equal(output.Bytes(), first) {
		t.Fatal("Write output differs from Marshal")
	}
}

func TestMarshalRejectsInvalidDocument(t *testing.T) {
	t.Parallel()

	document := validDocument()
	document.Tools[0].Confidence.Score = 1.1
	_, err := Marshal(document)
	if !IsValidationError(err) {
		t.Fatalf("error = %T %v, want ValidationError", err, err)
	}
}

func TestWriteReportsShortWrite(t *testing.T) {
	t.Parallel()

	err := Write(shortWriter{}, validDocument())
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v, want io.ErrShortWrite", err)
	}
}

func TestValidateContractInvariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Document)
		code   string
	}{
		{
			name: "invalid source",
			mutate: func(document *Document) {
				document.Source.Kind = "file"
			},
			code: "invalid_source_kind",
		},
		{
			name: "invalid coverage",
			mutate: func(document *Document) {
				document.FrameCoverage.Status = CoveragePartial
			},
			code: "missing_partial_coverage",
		},
		{
			name: "duplicate tool id",
			mutate: func(document *Document) {
				document.Tools = append(document.Tools, cloneTool(document.Tools[0]))
			},
			code: "duplicate_tool_id",
		},
		{
			name: "empty parameter name",
			mutate: func(document *Document) {
				document.Tools[0].Parameters[0].Name = ""
			},
			code: "required",
		},
		{
			name: "invalid parameter type",
			mutate: func(document *Document) {
				document.Tools[0].Parameters[0].Type = "date"
			},
			code: "invalid_value_type",
		},
		{
			name: "duplicate locator id",
			mutate: func(document *Document) {
				duplicate := document.Tools[0]
				duplicate.ID = "other-tool"
				duplicate.Locators = []LocatorCandidate{document.Tools[0].Locators[0]}
				document.Tools = append(document.Tools, duplicate)
			},
			code: "duplicate_locator_id",
		},
		{
			name: "unknown locator reference",
			mutate: func(document *Document) {
				document.Tools[0].Actions[0].LocatorCandidateIDs = []string{"missing"}
			},
			code: "unknown_locator",
		},
		{
			name: "unknown input parameter",
			mutate: func(document *Document) {
				document.Tools[0].Actions[0].InputParameter = "missing"
			},
			code: "unknown_parameter",
		},
		{
			name: "invalid action enum",
			mutate: func(document *Document) {
				document.Tools[0].Actions[0].Action = "hover"
			},
			code: "invalid_action_kind",
		},
		{
			name: "invalid side effect enum",
			mutate: func(document *Document) {
				document.Tools[0].Actions[0].SideEffect.Class = "destructive"
			},
			code: "invalid_side_effect",
		},
		{
			name: "invalid provenance enum",
			mutate: func(document *Document) {
				document.Tools[0].Provenance[0].Kind = "browser"
			},
			code: "invalid_provenance_kind",
		},
		{
			name: "unknown warning tool",
			mutate: func(document *Document) {
				document.Warnings = append(document.Warnings, Warning{
					Code: "test", Message: "test", ToolID: "missing", FramePath: []FrameReference{},
				})
			},
			code: "unknown_tool",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document := validDocument()
			test.mutate(document)
			err := document.Validate()
			var validationError *ValidationError
			if !errors.As(err, &validationError) || validationError.Code != test.code {
				t.Fatalf("error = %T %v, want ValidationError %q", err, err, test.code)
			}
		})
	}
}

func validDocument() *Document {
	document := NewDocument(SourceMetadata{
		Kind: SourceLaunchURL, ExecutionBoundary: ExecutionAgentOwned,
	})
	document.FrameCoverage.Status = CoverageComplete
	document.FrameCoverage.Frames = []CoveredFrame{{Path: []FrameReference{}}}
	document.Tools = []Tool{{
		ID: "search-123", Name: "Search", Description: "Search",
		Parameters: []Parameter{{
			Name: "query", Type: ValueString, Required: true,
			Enum: []string{}, Properties: []ParameterProperty{},
		}},
		Locators: []LocatorCandidate{{
			ID:        "search-123-locator-123",
			FramePath: []PathNode{}, ShadowPath: []PathNode{},
			Semantic: &SemanticLocator{
				Scope: []SemanticNode{}, Role: "searchbox", Name: "Search",
			},
			Confidence: Confidence{Score: 0.9},
			Provenance: []Provenance{{Kind: ProvenanceAccessibility, Reference: "ax:search"}},
		}},
		Actions: []ActionBinding{{
			Action: ActionFill, InputParameter: "query",
			LocatorCandidateIDs: []string{"search-123-locator-123"},
			SideEffect:          SideEffect{Class: SideEffectNone, SafeForExploration: true},
		}},
		Confidence: Confidence{Score: 0.9},
		Provenance: []Provenance{{Kind: ProvenanceAccessibility, Reference: "ax:form"}},
	}}
	return document
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) {
	return len(data) - 1, nil
}

package tir

import (
	"bytes"
	"encoding/json"
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

func TestCanonicalMatchesMarshalWithoutJSONRoundTrip(t *testing.T) {
	t.Parallel()

	document := validDocument()
	document.Tools[0].Parameters[0].Enum = []string{"z", "a", "z"}
	original := append([]string(nil), document.Tools[0].Parameters[0].Enum...)

	canonical, err := Canonical(document)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	if !equalStrings(document.Tools[0].Parameters[0].Enum, original) {
		t.Fatalf("Canonical mutated caller enum: %v", document.Tools[0].Parameters[0].Enum)
	}
	if !equalStrings(canonical.Tools[0].Parameters[0].Enum, []string{"a", "z"}) {
		t.Fatalf("canonical enum = %v, want sorted unique", canonical.Tools[0].Parameters[0].Enum)
	}

	fromCanonical, err := json.Marshal(canonical)
	if err != nil {
		t.Fatalf("marshal canonical: %v", err)
	}
	fromMarshal, err := Marshal(document)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Equal(fromCanonical, fromMarshal) {
		t.Fatal("Canonical plus json.Marshal differs from Marshal")
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
			name: "array missing items",
			mutate: func(document *Document) {
				document.Tools[0].Parameters[0].Type = ValueArray
			},
			code: "incompatible_shape",
		},
		{
			name: "object carrying enum",
			mutate: func(document *Document) {
				document.Tools[0].Parameters = []Parameter{{
					Name: "options", Type: ValueObject, Enum: []string{"a"},
				}}
			},
			code: "incompatible_shape",
		},
		{
			name: "string carrying properties",
			mutate: func(document *Document) {
				document.Tools[0].Parameters[0].Properties = []ParameterProperty{{
					Name: "nested", Shape: ParameterShape{Type: ValueString},
				}}
			},
			code: "incompatible_shape",
		},
		{
			name: "primitive carrying enum",
			mutate: func(document *Document) {
				document.Tools[0].Parameters = []Parameter{{
					Name: "flag", Type: ValueBoolean, Enum: []string{"true"},
				}}
			},
			code: "incompatible_shape",
		},
		{
			name: "invalid tool id",
			mutate: func(document *Document) {
				document.Tools[0].ID = "invalid name"
			},
			code: "invalid_tool_id",
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

func TestFramePathKeyIsUnambiguousAndNumeric(t *testing.T) {
	t.Parallel()

	left := []FrameReference{{Index: 0, Name: "a:b", Src: "c"}}
	right := []FrameReference{{Index: 0, Name: "a", Src: "b:c"}}
	if framePathKey(left) == framePathKey(right) {
		t.Fatal("page-controlled name and src must not collide through delimiters")
	}

	document := validDocument()
	document.FrameCoverage.Frames = []CoveredFrame{
		{Path: []FrameReference{{Index: 10, Name: "later"}}},
		{Path: []FrameReference{{Index: 2, Name: "earlier"}}},
	}
	canonicalize(document)
	if document.FrameCoverage.Frames[0].Path[0].Index != 2 {
		t.Fatalf("index order = %d, want 2 before 10", document.FrameCoverage.Frames[0].Path[0].Index)
	}

	distinct := validDocument()
	distinct.FrameCoverage.Frames = []CoveredFrame{
		{Path: []FrameReference{{Index: 0, Name: "a:b", Src: "c"}}},
		{Path: []FrameReference{{Index: 0, Name: "a", Src: "b:c"}}},
	}
	if err := distinct.Validate(); err != nil {
		t.Fatalf("distinct delimiter-bearing paths must not collide: %v", err)
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

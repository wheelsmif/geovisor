package compiler

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/wheelsmif/geovisor/internal/observation"
	"github.com/wheelsmif/geovisor/internal/tir"
)

func TestCompileAggregatesDeterministically(t *testing.T) {
	t.Parallel()

	document, err := Compile(fixtureInput())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if document.FrameCoverage.Status != tir.CoveragePartial {
		t.Fatalf("coverage = %q, want partial", document.FrameCoverage.Status)
	}
	if len(document.FrameCoverage.Frames) != 1 || len(document.FrameCoverage.Uncovered) != 1 {
		t.Fatalf("coverage frames = %d covered, %d uncovered", len(document.FrameCoverage.Frames), len(document.FrameCoverage.Uncovered))
	}
	if len(document.Tools) != 2 {
		t.Fatalf("tools = %d, want two distinct scoped tools", len(document.Tools))
	}

	search := findTool(t, document, "Search")
	if len(search.Parameters) != 2 {
		t.Fatalf("search parameters = %d, want deduplicated pair", len(search.Parameters))
	}
	if search.Parameters[0].Name != "query" || search.Parameters[1].Name != "category" {
		t.Fatalf("parameter order = %q, %q", search.Parameters[0].Name, search.Parameters[1].Name)
	}
	if len(search.Locators) != 2 {
		t.Fatalf("search locators = %d, want two", len(search.Locators))
	}
	if len(search.Actions) != 1 {
		t.Fatalf("search actions = %d, want deduplicated action", len(search.Actions))
	}
	if search.Actions[0].SideEffect.Class != tir.SideEffectNetwork {
		t.Fatalf("side effect = %q, want conservative network", search.Actions[0].SideEffect.Class)
	}
	if search.Confidence.Score != 0.96 {
		t.Fatalf("confidence = %v, want 0.96", search.Confidence.Score)
	}
	assertWarning(t, document, "ambiguous_side_effect")
	assertWarning(t, document, "css_fallback")
	assertWarning(t, document, "frame_uncovered")
}

func TestCompileDoesNotCollapseDistinctScopes(t *testing.T) {
	t.Parallel()

	input := fixtureInput()
	document, err := Compile(input)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var ids []string
	for _, tool := range document.Tools {
		if tool.Name == "Search" {
			ids = append(ids, tool.ID)
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("scoped controls must have distinct stable IDs, got %v", ids)
	}
}

func TestCompileRejectsInvalidRawReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Input)
		code   string
	}{
		{
			name: "negative frame index",
			mutate: func(input *Input) {
				input.Batches[0].Frames[0].Path = []observation.FrameReference{{Index: -1}}
			},
			code: "invalid_frame_index",
		},
		{
			name: "invalid locator index",
			mutate: func(input *Input) {
				input.Batches[0].Interactions[0].Actions[0].LocatorIndexes = []int{99}
			},
			code: "invalid_locator_index",
		},
		{
			name: "invalid evidence score",
			mutate: func(input *Input) {
				input.Batches[0].Interactions[0].Evidence[0].Score = 2
			},
			code: "invalid_confidence",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := fixtureInput()
			test.mutate(&input)
			_, err := Compile(input)
			var compilerError *Error
			if !errors.As(err, &compilerError) || compilerError.Code != test.code {
				t.Fatalf("error = %T %v, want compiler error %q", err, err, test.code)
			}
		})
	}
}

func TestCompileRepeatedShuffleIsByteIdentical(t *testing.T) {
	t.Parallel()

	input := fixtureInput()
	baselineDocument, err := Compile(input)
	if err != nil {
		t.Fatalf("compile baseline: %v", err)
	}
	baseline, err := tir.Marshal(baselineDocument)
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}

	random := rand.New(rand.NewSource(20260904))
	for iteration := 0; iteration < 100; iteration++ {
		shuffled := cloneInput(input)
		shuffleInput(random, &shuffled)
		document, err := Compile(shuffled)
		if err != nil {
			t.Fatalf("iteration %d compile: %v", iteration, err)
		}
		actual, err := tir.Marshal(document)
		if err != nil {
			t.Fatalf("iteration %d marshal: %v", iteration, err)
		}
		if !bytes.Equal(actual, baseline) {
			t.Fatalf("iteration %d produced different bytes\nwant: %s\n got: %s", iteration, baseline, actual)
		}
	}
}

func TestCompileGolden(t *testing.T) {
	t.Parallel()

	document, err := Compile(fixtureInput())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	actual, err := tir.Marshal(document)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join("testdata", "compiler.golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, append(actual, '\n'), 0o644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	expected = bytes.TrimSuffix(expected, []byte{'\n'})
	if !bytes.Equal(actual, expected) {
		t.Fatalf("golden mismatch\nwant: %s\n got: %s", expected, actual)
	}
}

func BenchmarkCompileAndMarshal(b *testing.B) {
	input := fixtureInput()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		document, err := Compile(input)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := tir.Marshal(document); err != nil {
			b.Fatal(err)
		}
	}
}

func fixtureInput() Input {
	order0, order1 := 0, 1
	root := observation.Frame{Accessible: true, URL: "https://example.test/", Origin: "https://example.test"}
	childPath := []observation.FrameReference{{Index: 0, Name: "payments", Src: "https://pay.example.test"}}
	search := observation.Interaction{
		Kind:        observation.InteractionForm,
		Scope:       []observation.SemanticNode{{Role: "region", Name: "Catalog"}},
		Role:        "search",
		Name:        "Search",
		Description: "Search the catalog",
		Parameters: []observation.Parameter{
			{Name: "category", Type: observation.ValueString, Enum: []string{"Books", "Games", "Books"}, SourceOrder: &order1},
			{Name: "query", Type: observation.ValueString, Required: true, SourceOrder: &order0},
		},
		Locators: []observation.Locator{
			{
				Semantic: &observation.SemanticLocator{Role: "searchbox", Name: "Search products"},
				Evidence: []observation.Evidence{{Kind: observation.EvidenceAccessibility, Reference: "ax:search", Score: 0.9}},
			},
			{
				CSS:      "#catalog-search",
				Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:#catalog-search", Score: 0.7}},
			},
		},
		Actions: []observation.Action{{
			Kind: observation.ActionFill, InputParameter: "query", LocatorIndexes: []int{0},
			SideEffect: observation.SideEffect{Class: observation.SideEffectNone, SafeForExploration: true},
		}},
		Evidence: []observation.Evidence{
			{Kind: observation.EvidenceAccessibility, Reference: "ax:form", Score: 0.9},
			{Kind: observation.EvidenceDOM, Reference: "dom:form", Score: 0.6},
		},
	}
	duplicate := search
	duplicate.Parameters = append([]observation.Parameter(nil), search.Parameters...)
	duplicate.Locators = append([]observation.Locator(nil), search.Locators...)
	duplicate.Actions = append([]observation.Action(nil), search.Actions...)
	duplicate.Actions[0].SideEffect = observation.SideEffect{
		Class: observation.SideEffectNetwork, SafeForExploration: false,
		Rationale: "may request suggestions",
	}
	duplicate.Evidence = []observation.Evidence{
		{Kind: observation.EvidenceAccessibility, Reference: "ax:form", Score: 0.9},
	}
	distinct := search
	distinct.Scope = []observation.SemanticNode{{Role: "region", Name: "Help"}}
	distinct.Description = "Search help"
	distinct.Parameters = append([]observation.Parameter(nil), search.Parameters...)
	distinct.Locators = append([]observation.Locator(nil), search.Locators[:1]...)
	distinct.Actions = append([]observation.Action(nil), search.Actions...)
	distinct.Evidence = append([]observation.Evidence(nil), search.Evidence...)

	return Input{
		Source: observation.Source{
			Kind: observation.SourceLaunchURL, RequestedURL: " https://example.test/ ",
			FinalURL: "https://example.test/",
		},
		Batches: []observation.Batch{
			{
				CoverageReported: true,
				Frames: []observation.Frame{
					root,
					{Path: childPath, Accessible: false, Reason: "cross-origin frame"},
				},
				Interactions: []observation.Interaction{search, distinct},
			},
			{
				CoverageReported: true,
				Frames:           []observation.Frame{root},
				Interactions:     []observation.Interaction{duplicate},
			},
		},
	}
}

func cloneInput(input Input) Input {
	data, err := json.Marshal(input)
	if err != nil {
		panic(err)
	}
	var result Input
	if err := json.Unmarshal(data, &result); err != nil {
		panic(err)
	}
	return result
}

func shuffleInput(random *rand.Rand, input *Input) {
	random.Shuffle(len(input.Batches), func(i, j int) {
		input.Batches[i], input.Batches[j] = input.Batches[j], input.Batches[i]
	})
	for batchIndex := range input.Batches {
		batch := &input.Batches[batchIndex]
		random.Shuffle(len(batch.Frames), func(i, j int) {
			batch.Frames[i], batch.Frames[j] = batch.Frames[j], batch.Frames[i]
		})
		random.Shuffle(len(batch.Interactions), func(i, j int) {
			batch.Interactions[i], batch.Interactions[j] = batch.Interactions[j], batch.Interactions[i]
		})
		for interactionIndex := range batch.Interactions {
			interaction := &batch.Interactions[interactionIndex]
			random.Shuffle(len(interaction.Parameters), func(i, j int) {
				interaction.Parameters[i], interaction.Parameters[j] = interaction.Parameters[j], interaction.Parameters[i]
			})
			random.Shuffle(len(interaction.Actions), func(i, j int) {
				interaction.Actions[i], interaction.Actions[j] = interaction.Actions[j], interaction.Actions[i]
			})
			random.Shuffle(len(interaction.Evidence), func(i, j int) {
				interaction.Evidence[i], interaction.Evidence[j] = interaction.Evidence[j], interaction.Evidence[i]
			})
			for parameterIndex := range interaction.Parameters {
				enum := interaction.Parameters[parameterIndex].Enum
				random.Shuffle(len(enum), func(i, j int) { enum[i], enum[j] = enum[j], enum[i] })
			}
			for locatorIndex := range interaction.Locators {
				evidence := interaction.Locators[locatorIndex].Evidence
				random.Shuffle(len(evidence), func(i, j int) { evidence[i], evidence[j] = evidence[j], evidence[i] })
			}
		}
	}
}

func findTool(t *testing.T, document *tir.Document, name string) tir.Tool {
	t.Helper()
	for _, tool := range document.Tools {
		if tool.Name == name && tool.Description == "Search the catalog" {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return tir.Tool{}
}

func assertWarning(t *testing.T, document *tir.Document, code string) {
	t.Helper()
	for _, warning := range document.Warnings {
		if warning.Code == code {
			return
		}
	}
	t.Fatalf("warning %q not found", code)
}

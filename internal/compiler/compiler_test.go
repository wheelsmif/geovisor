package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/wheelsmif/geovisor/internal/observation"
	"github.com/wheelsmif/geovisor/internal/tir"
)

func TestCompileAggregatesDeterministically(t *testing.T) {
	t.Parallel()

	document, err := Compile(context.Background(), fixtureInput())
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
	assertActionsReferenceCompiledLocators(t, search)
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

func TestCompileCopiesObservationWarnings(t *testing.T) {
	t.Parallel()

	input := fixtureInput()
	input.Batches[0].Warnings = []observation.Warning{
		{
			Code:    observation.WarningElementExtractionFailed,
			Message: "2 element(s) could not be extracted",
		},
		{
			Code:    observation.WarningClosedShadowRoot,
			Message: "closed shadow root was not readable",
		},
	}
	document, err := Compile(context.Background(), input)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertWarning(t, document, observation.WarningElementExtractionFailed)
	assertWarning(t, document, observation.WarningClosedShadowRoot)
}

func TestCompileDoesNotCollapseDistinctScopes(t *testing.T) {
	t.Parallel()

	input := fixtureInput()
	document, err := Compile(context.Background(), input)
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
		field  string
	}{
		{
			name: "negative frame index",
			mutate: func(input *Input) {
				input.Batches[0].Frames[0].Path = []observation.FrameReference{{Index: -1}}
			},
			code:  "invalid_frame_index",
			field: "batches[0].frames[0].path[0].index",
		},
		{
			name: "invalid locator index",
			mutate: func(input *Input) {
				input.Batches[0].Interactions[0].Actions[0].LocatorIndexes = []int{99}
			},
			code:  "invalid_locator_index",
			field: "batches[0].interactions[0].actions[0].locatorIndexes[0]",
		},
		{
			name: "invalid evidence score",
			mutate: func(input *Input) {
				input.Batches[0].Interactions[0].Evidence[0].Score = 2
			},
			code:  "invalid_confidence",
			field: "batches[0].interactions[0].evidence[0].score",
		},
		{
			name: "invalid source kind",
			mutate: func(input *Input) {
				input.Source.Kind = "file"
			},
			code:  "invalid_source_kind",
			field: "source.kind",
		},
		{
			name: "invalid parameter type",
			mutate: func(input *Input) {
				input.Batches[0].Interactions[0].Parameters[0].Type = "date"
			},
			code:  "invalid_value_type",
			field: "batches[0].interactions[0].parameters[0].type",
		},
		{
			name: "invalid nested shape type",
			mutate: func(input *Input) {
				input.Batches[0].Interactions[0].Parameters[0].Items = &observation.ParameterShape{Type: "date"}
			},
			code:  "invalid_value_type",
			field: "batches[0].interactions[0].parameters[0].items.type",
		},
		{
			name: "invalid action kind",
			mutate: func(input *Input) {
				input.Batches[0].Interactions[0].Actions[0].Kind = "hover"
			},
			code:  "invalid_action_kind",
			field: "batches[0].interactions[0].actions[0].kind",
		},
		{
			name: "invalid side-effect class",
			mutate: func(input *Input) {
				input.Batches[0].Interactions[0].Actions[0].SideEffect.Class = "destructive"
			},
			code:  "invalid_side_effect",
			field: "batches[0].interactions[0].actions[0].sideEffect.class",
		},
		{
			name: "unknown marked safe to explore",
			mutate: func(input *Input) {
				input.Batches[0].Interactions[0].Actions[0].SideEffect = observation.SideEffect{
					Class: observation.SideEffectUnknown, SafeForExploration: true,
				}
			},
			code:  "unsafe_exploration",
			field: "batches[0].interactions[0].actions[0].sideEffect",
		},
		{
			name: "invalid evidence kind",
			mutate: func(input *Input) {
				input.Batches[0].Interactions[0].Evidence[0].Kind = "browser"
			},
			code:  "invalid_provenance_kind",
			field: "batches[0].interactions[0].evidence[0].kind",
		},
		{
			name: "negative semantic nth",
			mutate: func(input *Input) {
				input.Batches[0].Interactions[0].Locators[0].Semantic.Nth = -1
			},
			code:  "invalid_nth",
			field: "batches[0].interactions[0].locators[0].semantic.nth",
		},
		{
			name: "negative scope nth",
			mutate: func(input *Input) {
				input.Batches[0].Interactions[0].Scope[0].Nth = -1
			},
			code:  "invalid_nth",
			field: "batches[0].interactions[0].scope[0].nth",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := fixtureInput()
			test.mutate(&input)
			_, err := Compile(context.Background(), input)
			var compilerError *Error
			if !errors.As(err, &compilerError) || compilerError.Code != test.code {
				t.Fatalf("error = %T %v, want compiler error %q", err, err, test.code)
			}
			if compilerError.Field != test.field {
				t.Fatalf("field = %q, want %q", compilerError.Field, test.field)
			}
		})
	}
}

func TestCompileRepeatedShuffleIsByteIdentical(t *testing.T) {
	t.Parallel()

	input := fixtureInput()
	baselineDocument, err := Compile(context.Background(), input)
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
		document, err := Compile(context.Background(), shuffled)
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

func TestCompileIDsStableWhenUnrelatedInteractionInserted(t *testing.T) {
	t.Parallel()

	unnamed := observation.Interaction{
		Kind: observation.InteractionControl,
		Role: "textbox",
		Locators: []observation.Locator{{
			Semantic: &observation.SemanticLocator{Role: "textbox"},
			Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:input", Score: 0.7}},
		}},
		Actions: []observation.Action{{
			Kind:       observation.ActionFill,
			SideEffect: observation.SideEffect{Class: observation.SideEffectUnknown},
		}},
		Evidence: []observation.Evidence{{Kind: observation.EvidenceHeuristic, Reference: "name:fallback", Score: 0.35}},
	}
	unrelated := observation.Interaction{
		Kind: observation.InteractionAction,
		Role: "button",
		Name: "Elsewhere",
		Locators: []observation.Locator{{
			Semantic: &observation.SemanticLocator{Role: "button", Name: "Elsewhere"},
			Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:button", Score: 0.7}},
		}},
		Actions: []observation.Action{{
			Kind:       observation.ActionClick,
			SideEffect: observation.SideEffect{Class: observation.SideEffectUnknown},
		}},
		Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:button", Score: 0.6}},
	}

	baseline, err := Compile(context.Background(), Input{
		Source:  observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{Interactions: []observation.Interaction{unnamed}}},
	})
	if err != nil {
		t.Fatalf("compile baseline: %v", err)
	}
	if len(baseline.Tools) != 1 {
		t.Fatalf("baseline tools = %d, want 1", len(baseline.Tools))
	}

	shifted, err := Compile(context.Background(), Input{
		Source:  observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{Interactions: []observation.Interaction{unrelated, unnamed}}},
	})
	if err != nil {
		t.Fatalf("compile shifted: %v", err)
	}
	if toolID(t, shifted, baseline.Tools[0].Name, baseline.Tools[0].Description) != baseline.Tools[0].ID {
		t.Fatalf(
			"unrelated earlier interaction changed tool ID from %q to %q",
			baseline.Tools[0].ID,
			toolID(t, shifted, baseline.Tools[0].Name, baseline.Tools[0].Description),
		)
	}
}

func TestCompileGolden(t *testing.T) {
	t.Parallel()

	document, err := Compile(context.Background(), fixtureInput())
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

func TestCompileHonorsCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Compile(ctx, fixtureInput())
	var compilerError *Error
	if !errors.As(err, &compilerError) || compilerError.Code != "canceled" {
		t.Fatalf("error = %T %v, want canceled compiler error", err, err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled compile must unwrap to context.Canceled, got %v", err)
	}
}

func TestCompileIdentityIgnoresDisplayNameOrdinals(t *testing.T) {
	t.Parallel()

	control := func(name string, nth int) observation.Interaction {
		return observation.Interaction{
			Kind:  observation.InteractionControl,
			Role:  "textbox",
			Name:  name,
			Scope: []observation.SemanticNode{{Role: "region", Name: "Filters"}},
			Parameters: []observation.Parameter{
				{Name: "query", Type: observation.ValueString},
			},
			Locators: []observation.Locator{{
				Semantic: &observation.SemanticLocator{
					Scope: []observation.SemanticNode{{Role: "region", Name: "Filters"}},
					Role:  "textbox",
					Name:  "Query",
					Nth:   nth,
				},
				Evidence: []observation.Evidence{{Kind: observation.EvidenceAccessibility, Reference: "ax", Score: 0.9}},
			}},
			Actions: []observation.Action{{
				Kind: observation.ActionFill, InputParameter: "query",
				SideEffect: observation.SideEffect{Class: observation.SideEffectUnknown},
			}},
			Evidence: []observation.Evidence{{Kind: observation.EvidenceAccessibility, Reference: "ax", Score: 0.9}},
		}
	}
	compileNames := func(first, second observation.Interaction) [2]string {
		t.Helper()
		document, err := Compile(context.Background(), Input{
			Source: observation.Source{Kind: observation.SourceLaunchURL},
			Batches: []observation.Batch{{
				Interactions: []observation.Interaction{first, second},
			}},
		})
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if len(document.Tools) != 2 {
			t.Fatalf("tools = %d, want 2", len(document.Tools))
		}
		return [2]string{document.Tools[0].ID, document.Tools[1].ID}
	}

	baseline := compileNames(control("Query", 0), control("Query (2)", 1))
	renamed := compileNames(control("Query (2)", 0), control("Query (3)", 1))
	if baseline != renamed {
		t.Fatalf("display-name ordinals changed IDs: %v vs %v", baseline, renamed)
	}
}

func TestCompileCollapsesSameFrameLinksIntoFollowLink(t *testing.T) {
	t.Parallel()

	document, err := Compile(context.Background(), Input{
		Source: observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{
			Interactions: []observation.Interaction{
				linkInteraction("CSS", "/wiki/CSS"),
				linkInteraction("HTML", "/wiki/HTML"),
			},
		}},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(document.Tools) != 1 {
		t.Fatalf("tools = %d, want one family tool", len(document.Tools))
	}
	tool := document.Tools[0]
	if tool.Name != "Follow link" {
		t.Fatalf("name = %q, want Follow link", tool.Name)
	}
	if len(tool.Parameters) != 1 || tool.Parameters[0].Name != "target" || !tool.Parameters[0].Required {
		t.Fatalf("parameters = %#v, want required target", tool.Parameters)
	}
	if got := tool.Parameters[0].Enum; len(got) != 2 || got[0] != "CSS" || got[1] != "HTML" {
		t.Fatalf("target enum = %v, want CSS, HTML", tool.Parameters[0].Enum)
	}
	if len(tool.Locators) != 2 {
		t.Fatalf("locators = %d, want 2", len(tool.Locators))
	}
	if len(tool.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(tool.Actions))
	}
	action := tool.Actions[0]
	if action.Action != tir.ActionClick || action.InputParameter != "target" {
		t.Fatalf("action = %#v, want click with inputParameter target", action)
	}
	if action.SideEffect.Class != tir.SideEffectNavigation || action.SideEffect.SafeForExploration {
		t.Fatalf("side effect = %#v, want unsafe navigation", action.SideEffect)
	}
	if len(action.LocatorCandidateIDs) != 2 {
		t.Fatalf("locator refs = %d, want both family members", len(action.LocatorCandidateIDs))
	}
	assertActionsReferenceCompiledLocators(t, tool)
}

func TestCompileKeepsUniqueButtonAsSingleton(t *testing.T) {
	t.Parallel()

	document, err := Compile(context.Background(), Input{
		Source: observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{
			Interactions: []observation.Interaction{
				buttonInteraction("Save", observation.SideEffectUnknown),
				linkInteraction("HTML", "/wiki/HTML"),
			},
		}},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	save := findToolByName(t, document, "Save")
	if len(save.Parameters) != 0 {
		t.Fatalf("unique Save must not grow a target parameter, got %#v", save.Parameters)
	}
	if save.Actions[0].InputParameter != "" {
		t.Fatalf("unique Save inputParameter = %q, want empty", save.Actions[0].InputParameter)
	}
	follow := findToolByName(t, document, "Follow link")
	if follow.Parameters[0].Name != "target" {
		t.Fatalf("single leftover link must still be a family tool, got %#v", follow.Parameters)
	}
}

func TestCompileDropsLowValueActions(t *testing.T) {
	t.Parallel()

	document, err := Compile(context.Background(), Input{
		Source: observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{
			Interactions: []observation.Interaction{
				linkInteraction("[1]", "#cite_note-1"),
				linkInteraction("10.1000/xyz123", "https://doi.org/10.1000/xyz123"),
				linkInteraction("RFC 9110", "/wiki/RFC_9110"),
				{
					Kind: observation.InteractionAction,
					Role: "link",
					Name: "See also",
					Locators: []observation.Locator{{
						Semantic: &observation.SemanticLocator{Role: "link", Name: "See also"},
						CSS:      `a[href="#cite_note-HTML-1"]`,
						Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:a", Score: 0.7}},
					}},
					Actions: []observation.Action{{
						Kind:       observation.ActionClick,
						SideEffect: observation.SideEffect{Class: observation.SideEffectNavigation},
					}},
					Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:a", Score: 0.6}},
				},
				{
					Kind: observation.InteractionAction,
					Role: "link",
					Locators: []observation.Locator{{
						Semantic: &observation.SemanticLocator{Role: "link"},
						Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:a", Score: 0.7}},
					}},
					Actions: []observation.Action{{
						Kind:       observation.ActionClick,
						SideEffect: observation.SideEffect{Class: observation.SideEffectNavigation},
					}},
					Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:a", Score: 0.6}},
				},
				linkInteraction("HTML", "/wiki/HTML"),
			},
		}},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(document.Tools) != 1 {
		t.Fatalf("tools = %d, want only the named article link", len(document.Tools))
	}
	tool := document.Tools[0]
	if tool.Name != "Follow link" {
		t.Fatalf("kept tool = %q, want Follow link", tool.Name)
	}
	if got := tool.Parameters[0].Enum; len(got) != 1 || got[0] != "HTML" {
		t.Fatalf("target enum = %v, want HTML", tool.Parameters[0].Enum)
	}
	assertWarning(t, document, warningDroppedLowValueAction)
}

func TestCompileFamilyIDStableWhenUnrelatedSingletonInserted(t *testing.T) {
	t.Parallel()

	links := []observation.Interaction{
		linkInteraction("CSS", "/wiki/CSS"),
		linkInteraction("HTML", "/wiki/HTML"),
	}
	baseline, err := Compile(context.Background(), Input{
		Source:  observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{Interactions: links}},
	})
	if err != nil {
		t.Fatalf("compile baseline: %v", err)
	}
	if len(baseline.Tools) != 1 {
		t.Fatalf("baseline tools = %d, want 1", len(baseline.Tools))
	}

	shifted, err := Compile(context.Background(), Input{
		Source: observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{
			Interactions: []observation.Interaction{
				buttonInteraction("Elsewhere", observation.SideEffectUnknown),
				links[0],
				links[1],
			},
		}},
	})
	if err != nil {
		t.Fatalf("compile shifted: %v", err)
	}
	if toolID(t, shifted, baseline.Tools[0].Name, baseline.Tools[0].Description) != baseline.Tools[0].ID {
		t.Fatalf(
			"unrelated earlier singleton changed family ID from %q to %q",
			baseline.Tools[0].ID,
			toolID(t, shifted, baseline.Tools[0].Name, baseline.Tools[0].Description),
		)
	}
}

func TestCompileDoesNotFoldSubmitIntoFamilyOrForm(t *testing.T) {
	t.Parallel()

	order0 := 0
	form := observation.Interaction{
		Kind: observation.InteractionForm,
		Role: "form",
		Name: "Login",
		Parameters: []observation.Parameter{
			{Name: "user", Type: observation.ValueString, Required: true, SourceOrder: &order0},
		},
		Locators: []observation.Locator{{
			Semantic: &observation.SemanticLocator{Role: "textbox", Name: "User"},
			Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:input", Score: 0.7}},
		}},
		Actions: []observation.Action{{
			Kind: observation.ActionFill, InputParameter: "user",
			SideEffect: observation.SideEffect{Class: observation.SideEffectUnknown},
		}},
		Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:form", Score: 0.6}},
	}
	save := buttonInteraction("Save", observation.SideEffectSubmission)
	save.Locators[0].CSS = `button[type="submit"]`
	continueSave := buttonInteraction("Save and continue", observation.SideEffectSubmission)
	continueSave.Locators[0].CSS = `button[type="submit"]`

	document, err := Compile(context.Background(), Input{
		Source: observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{
			Interactions: []observation.Interaction{form, save, continueSave},
		}},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(document.Tools) != 3 {
		t.Fatalf("tools = %d, want form plus two standalone submits", len(document.Tools))
	}
	login := findToolByName(t, document, "Login")
	if len(login.Parameters) != 1 || login.Parameters[0].Name != "user" {
		t.Fatalf("form parameters = %#v, want fill-only user", login.Parameters)
	}
	for _, action := range login.Actions {
		if action.Action == tir.ActionClick {
			t.Fatal("form tool must stay fill-only")
		}
	}
	findToolByName(t, document, "Save")
	findToolByName(t, document, "Save and continue")
}

func TestCompileFamilyTargetOrdinalsAreBijective(t *testing.T) {
	t.Parallel()

	first := linkInteraction("Next", "/a")
	first.Locators[0].Semantic.Nth = 0
	second := linkInteraction("Next", "/b")
	second.Name = "Next (2)"
	second.Locators[0].Semantic.Nth = 1

	document, err := Compile(context.Background(), Input{
		Source:  observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{Interactions: []observation.Interaction{first, second}}},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	tool := findToolByName(t, document, "Follow link")
	if got := tool.Parameters[0].Enum; len(got) != 2 || got[0] != "Next" || got[1] != "Next (2)" {
		t.Fatalf("target enum = %v, want Next, Next (2)", tool.Parameters[0].Enum)
	}
	if len(tool.Locators) != 2 {
		t.Fatalf("locators = %d, want one per member", len(tool.Locators))
	}
}

func TestCompileOmitsFamilyEnumAboveCap(t *testing.T) {
	t.Parallel()

	interactions := make([]observation.Interaction, 0, familyEnumCap+1)
	for i := 0; i < familyEnumCap+1; i++ {
		name := fmt.Sprintf("Link %02d", i)
		interactions = append(interactions, linkInteraction(name, "/"+name))
	}
	document, err := Compile(context.Background(), Input{
		Source:  observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{Interactions: interactions}},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	tool := findToolByName(t, document, "Follow link")
	if len(tool.Parameters[0].Enum) != 0 {
		t.Fatalf("enum length = %d, want omitted above cap", len(tool.Parameters[0].Enum))
	}
	if tool.Description != familyToolDescription() {
		t.Fatalf("description = %q, want target-name guidance", tool.Description)
	}
	if len(tool.Locators) != familyEnumCap+1 {
		t.Fatalf("locators = %d, want every family member", len(tool.Locators))
	}
}

func BenchmarkCompileAndMarshal(b *testing.B) {
	input := fixtureInput()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		document, err := Compile(context.Background(), input)
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

func toolID(t *testing.T, document *tir.Document, name, description string) string {
	t.Helper()
	for _, tool := range document.Tools {
		if tool.Name == name && tool.Description == description {
			return tool.ID
		}
	}
	t.Fatalf("tool %q (%q) not found", name, description)
	return ""
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

func findToolByName(t *testing.T, document *tir.Document, name string) tir.Tool {
	t.Helper()
	for _, tool := range document.Tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found among %v", name, toolNames(document))
	return tir.Tool{}
}

func toolNames(document *tir.Document) []string {
	names := make([]string, 0, len(document.Tools))
	for _, tool := range document.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func linkInteraction(name, href string) observation.Interaction {
	css := "a"
	if href != "" {
		css = fmt.Sprintf("a[href=%q]", href)
	}
	return observation.Interaction{
		Kind: observation.InteractionAction,
		Role: "link",
		Name: name,
		Locators: []observation.Locator{{
			Semantic: &observation.SemanticLocator{Role: "link", Name: name},
			CSS:      css,
			Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:a", Score: 0.7}},
		}},
		Actions: []observation.Action{{
			Kind:       observation.ActionClick,
			SideEffect: observation.SideEffect{Class: observation.SideEffectNavigation},
		}},
		Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:a", Score: 0.6}},
	}
}

func buttonInteraction(name string, class observation.SideEffectClass) observation.Interaction {
	return observation.Interaction{
		Kind: observation.InteractionAction,
		Role: "button",
		Name: name,
		Locators: []observation.Locator{{
			Semantic: &observation.SemanticLocator{Role: "button", Name: name},
			Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:button", Score: 0.7}},
		}},
		Actions: []observation.Action{{
			Kind:       observation.ActionClick,
			SideEffect: observation.SideEffect{Class: class},
		}},
		Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:button", Score: 0.6}},
	}
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

func assertActionsReferenceCompiledLocators(t *testing.T, tool tir.Tool) {
	t.Helper()
	ids := make(map[string]struct{}, len(tool.Locators))
	for _, locator := range tool.Locators {
		ids[locator.ID] = struct{}{}
	}
	for _, action := range tool.Actions {
		for _, id := range action.LocatorCandidateIDs {
			if _, exists := ids[id]; !exists {
				t.Fatalf("action %q references locator %q, which was not compiled onto the tool", action.Action, id)
			}
		}
	}
}

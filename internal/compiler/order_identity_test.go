package compiler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wheelsmif/geovisor/internal/observation"
	"github.com/wheelsmif/geovisor/internal/tir"
)

func TestCompilePreservesSelectAndToolSourceOrder(t *testing.T) {
	t.Parallel()

	order := 0
	selectControl := observation.Interaction{
		Kind: observation.InteractionControl,
		Role: "combobox",
		Name: "Size",
		Parameters: []observation.Parameter{{
			Name: "size", Type: observation.ValueString, Required: true, SourceOrder: &order,
			Enum: []string{"Small", "Medium", "Large"},
		}},
		Locators: []observation.Locator{{
			Semantic: &observation.SemanticLocator{Role: "combobox", Name: "Size"},
			Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:select", Score: 0.8}},
		}},
		Actions: []observation.Action{{
			Kind: observation.ActionSelect, InputParameter: "size",
			SideEffect: observation.SideEffect{Class: observation.SideEffectLocalState, SafeForExploration: true},
		}},
		Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:select", Score: 0.8}},
	}
	document, err := Compile(context.Background(), Input{
		Source: observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{
			Interactions: []observation.Interaction{
				selectControl,
				namedControl("Zebra"),
				namedControl("Apple"),
			},
		}},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(document.Tools) != 3 {
		t.Fatalf("tools = %v, want three in source order", toolNames(document))
	}
	if document.Tools[0].Name != "Size" || document.Tools[1].Name != "Zebra" || document.Tools[2].Name != "Apple" {
		t.Fatalf("tool order = %v, want Size, Zebra, Apple", toolNames(document))
	}
	got := document.Tools[0].Parameters[0].Enum
	if len(got) != 3 || got[0] != "Small" || got[1] != "Medium" || got[2] != "Large" {
		t.Fatalf("enum = %v, want Small, Medium, Large", got)
	}
}

func namedControl(name string) observation.Interaction {
	order := 0
	return observation.Interaction{
		Kind: observation.InteractionControl,
		Role: "textbox",
		Name: name,
		Parameters: []observation.Parameter{{
			Name: name, Type: observation.ValueString, SourceOrder: &order,
		}},
		Locators: []observation.Locator{{
			Semantic: &observation.SemanticLocator{Role: "textbox", Name: name},
			Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:input", Score: 0.8}},
		}},
		Actions: []observation.Action{{
			Kind: observation.ActionFill, InputParameter: name,
			SideEffect: observation.SideEffect{Class: observation.SideEffectNone, SafeForExploration: true},
		}},
		Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:input", Score: 0.8}},
	}
}

func TestCompileParameterCaseTieBreak(t *testing.T) {
	t.Parallel()

	order := 0
	document, err := Compile(context.Background(), Input{
		Source: observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{
			Interactions: []observation.Interaction{{
				Kind: observation.InteractionForm,
				Role: "form",
				Name: "Filter",
				Parameters: []observation.Parameter{
					{Name: "query", Type: observation.ValueString, SourceOrder: &order},
					{Name: "Query", Type: observation.ValueString, SourceOrder: &order},
				},
				Locators: []observation.Locator{{
					Semantic: &observation.SemanticLocator{Role: "form", Name: "Filter"},
					Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:form", Score: 0.6}},
				}},
				Actions: []observation.Action{
					{Kind: observation.ActionFill, InputParameter: "query", SideEffect: observation.SideEffect{Class: observation.SideEffectNone, SafeForExploration: true}},
					{Kind: observation.ActionFill, InputParameter: "Query", SideEffect: observation.SideEffect{Class: observation.SideEffectNone, SafeForExploration: true}},
				},
				Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:form", Score: 0.6}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	tool := findToolByName(t, document, "Filter")
	if len(tool.Parameters) != 2 || tool.Parameters[0].Name != "Query" || tool.Parameters[1].Name != "query" {
		t.Fatalf("parameter order = %#v, want Query then query", tool.Parameters)
	}
}

func TestCompileShadowCSSDoesNotChangeToolID(t *testing.T) {
	t.Parallel()

	base := observation.Interaction{
		Kind: observation.InteractionAction,
		Role: "button",
		Name: "Save",
		Locators: []observation.Locator{{
			ShadowPath: []observation.PathNode{{
				Semantic: &observation.SemanticNode{Role: "generic", Name: "Host"},
				CSS:      "div:nth-of-type(1)",
			}},
			Semantic: &observation.SemanticLocator{Role: "button", Name: "Save"},
			Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:button", Score: 0.7}},
		}},
		Actions: []observation.Action{{
			Kind:       observation.ActionClick,
			SideEffect: observation.SideEffect{Class: observation.SideEffectUnknown},
		}},
		Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:button", Score: 0.6}},
	}
	shifted := base
	shifted.Locators = []observation.Locator{{
		ShadowPath: []observation.PathNode{{
			Semantic: &observation.SemanticNode{Role: "generic", Name: "Host"},
			CSS:      "div:nth-of-type(2)",
		}},
		Semantic: &observation.SemanticLocator{Role: "button", Name: "Save"},
		Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:button", Score: 0.7}},
	}}
	first, err := Compile(context.Background(), Input{
		Source:  observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{Interactions: []observation.Interaction{base}}},
	})
	if err != nil {
		t.Fatalf("compile baseline: %v", err)
	}
	second, err := Compile(context.Background(), Input{
		Source:  observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{Interactions: []observation.Interaction{shifted}}},
	})
	if err != nil {
		t.Fatalf("compile shifted sibling: %v", err)
	}
	if first.Tools[0].ID != second.Tools[0].ID {
		t.Fatalf("shadow-host CSS changed tool ID from %q to %q", first.Tools[0].ID, second.Tools[0].ID)
	}
	if second.Tools[0].Locators[0].ShadowPath[0].CSSFallback == "" {
		t.Fatal("CSS fallback was removed from the locator")
	}
}

func TestCompileCSSCitationPatternIsNotLowValue(t *testing.T) {
	t.Parallel()

	button := buttonInteraction("Save", observation.SideEffectUnknown)
	button.Locators[0].CSS = "#fn-save"
	document, err := Compile(context.Background(), Input{
		Source:  observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{Interactions: []observation.Interaction{button}}},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(document.Tools) != 1 || document.Tools[0].Name != "Save" {
		t.Fatalf("tools = %v, want Save kept", toolNames(document))
	}
}

func TestCompileTargetCheckboxStaysRankedBesideFamilyTarget(t *testing.T) {
	t.Parallel()

	order := 0
	checkbox := observation.Interaction{
		Kind: observation.InteractionControl,
		Role: "checkbox",
		Name: "Target",
		Parameters: []observation.Parameter{{
			Name: "target", Type: observation.ValueBoolean, SourceOrder: &order,
		}},
		Locators: []observation.Locator{{
			Semantic: &observation.SemanticLocator{Role: "checkbox", Name: "Target"},
			Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:input", Score: 0.8}},
		}},
		Actions: []observation.Action{{
			Kind: observation.ActionCheck, InputParameter: "target",
			SideEffect: observation.SideEffect{Class: observation.SideEffectLocalState, SafeForExploration: true},
		}},
		Evidence: []observation.Evidence{{Kind: observation.EvidenceDOM, Reference: "dom:input", Score: 0.8}},
	}
	document, err := Compile(context.Background(), Input{
		Source: observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{
			Interactions: []observation.Interaction{
				checkbox,
				linkInteraction("Home", "/home"),
				linkInteraction("Next", "/next"),
			},
		}},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	target := findToolByName(t, document, "Target")
	if target.Actions[0].LocatorSelection != tir.LocatorSelectionRanked || target.Actions[0].InputParameter != "target" {
		t.Fatalf("checkbox binding = %#v, want ranked target", target.Actions[0])
	}
	if target.Parameters[0].Type != tir.ValueBoolean {
		t.Fatalf("checkbox parameter type = %s, want boolean", target.Parameters[0].Type)
	}
	family := findToolByName(t, document, "Follow link")
	if family.Actions[0].LocatorSelection != tir.LocatorSelectionByTargetName {
		t.Fatalf("family selection = %q, want by_target_name", family.Actions[0].LocatorSelection)
	}
	if !strings.Contains(family.Description, "Follow one of this page's links") {
		t.Fatalf("family description = %q", family.Description)
	}
}

func TestValidateRejectsToolWithNoActions(t *testing.T) {
	t.Parallel()

	document := tir.NewDocument(tir.SourceMetadata{
		Kind: tir.SourceLaunchURL, ExecutionBoundary: tir.ExecutionAgentOwned,
	})
	document.FrameCoverage.Status = tir.CoverageComplete
	document.FrameCoverage.Frames = []tir.CoveredFrame{{Path: []tir.FrameReference{}}}
	document.Tools = []tir.Tool{{
		ID: "empty", Name: "Empty",
		Locators: []tir.LocatorCandidate{{
			ID: "empty-locator", CSSFallback: "#empty", Confidence: tir.Confidence{Score: 1},
		}},
		Confidence: tir.Confidence{Score: 1},
	}}
	document.Normalize()
	err := document.Validate()
	var validation *tir.ValidationError
	if err == nil || !errors.As(err, &validation) || validation.Code != tir.CodeMissingAction {
		t.Fatalf("error = %v, want missing_action", err)
	}
}

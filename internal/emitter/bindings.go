package emitter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/geo-suite/geovisor/internal/tir"
)

const bindingSchemaVersion = "1.0.0"

type bindingArtifact struct {
	SchemaVersion string              `json:"schemaVersion"`
	Target        Format              `json:"target"`
	Tools         []toolBindingRecipe `json:"tools"`
}

type toolBindingRecipe struct {
	ToolID  string                `json:"toolId"`
	Actions []actionBindingRecipe `json:"actions"`
}

type actionBindingRecipe struct {
	Action         tir.ActionKind         `json:"action"`
	InputParameter string                 `json:"inputParameter,omitempty"`
	SideEffect     tir.SideEffect         `json:"sideEffect"`
	Locators       []tir.LocatorCandidate `json:"locatorCandidates"`
}

// BindingArtifact emits the deterministic browser-action companion used by
// non-executable MCP and OpenAI definitions.
func BindingArtifact(
	ctx context.Context,
	document *tir.Document,
	target Format,
) (Artifact, error) {
	if target != FormatMCP && target != FormatOpenAI {
		return Artifact{}, failure(target, CodeUnsupportedFormat, "target", "bindings are available only for MCP and OpenAI")
	}
	canonical, err := canonicalDocument(ctx, target, document)
	if err != nil {
		return Artifact{}, err
	}
	return bindingArtifactForCanonical(ctx, canonical, target)
}

func bindingArtifactForCanonical(
	ctx context.Context,
	document *tir.Document,
	target Format,
) (Artifact, error) {
	bindings := bindingArtifact{
		SchemaVersion: bindingSchemaVersion,
		Target:        target,
		Tools:         make([]toolBindingRecipe, 0, len(document.Tools)),
	}
	for ti := range document.Tools {
		if err := canceled(ctx, target); err != nil {
			return Artifact{}, err
		}
		tool := &document.Tools[ti]
		recipe := toolBindingRecipe{
			ToolID:  tool.ID,
			Actions: make([]actionBindingRecipe, 0, len(tool.Actions)),
		}
		locators := make(map[string]tir.LocatorCandidate, len(tool.Locators))
		for _, locator := range tool.Locators {
			locators[locator.ID] = locator
		}
		for ai, action := range tool.Actions {
			actionRecipe := actionBindingRecipe{
				Action:         action.Action,
				InputParameter: action.InputParameter,
				SideEffect:     action.SideEffect,
				Locators:       make([]tir.LocatorCandidate, 0, len(action.LocatorCandidateIDs)),
			}
			for ri, locatorID := range action.LocatorCandidateIDs {
				locator, ok := locators[locatorID]
				if !ok {
					field := fmt.Sprintf(
						"tools[%q].actions[%d].locatorCandidateIds[%d]",
						tool.ID,
						ai,
						ri,
					)
					return Artifact{}, failure(target, CodeInvalidDocument, field, "locator reference is not defined")
				}
				actionRecipe.Locators = append(actionRecipe.Locators, locator)
			}
			recipe.Actions = append(recipe.Actions, actionRecipe)
		}
		bindings.Tools = append(bindings.Tools, recipe)
	}
	data, err := json.Marshal(bindings)
	if err != nil {
		return Artifact{}, wrap(target, CodeMarshal, "", err)
	}
	return Artifact{
		Name:      "tools." + string(target) + ".bindings.json",
		MediaType: "application/json",
		Data:      data,
	}, nil
}

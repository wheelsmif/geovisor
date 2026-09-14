package emitter

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/wheelsmif/geovisor/internal/pageurl"
	"github.com/wheelsmif/geovisor/internal/tir"
)

// webMCPRuntime is generated from client/src/webmcp-runtime.ts by the pinned
// esbuild toolchain, which is also what builds the extraction payload. Sharing
// one source tree is what keeps role resolution, accessible-name computation,
// and element addressing identical on the producer and consumer sides; when the
// runtime was a Go string literal it had silently drifted from the extractor
// (GV-001, GV-003, GV-004, GV-049).
//
//go:embed webmcp-runtime.js
var webMCPRuntime string

// WebMCPAPIVersion identifies the browser API used by generated modules.
const WebMCPAPIVersion = "Chrome 153 imperative API, updated 2026-09-01"

// WebMCP emits an ES module that registers executable browser-local tools.
type WebMCP struct{}

func (WebMCP) Format() Format {
	return FormatWebMCP
}

type webMCPDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema *jsonSchema            `json:"inputSchema"`
	Annotations webMCPAnnotations      `json:"annotations"`
	Actions     []tir.ActionBinding    `json:"actions"`
	Locators    []tir.LocatorCandidate `json:"locators"`
}

type webMCPAnnotations struct {
	ReadOnlyHint         bool `json:"readOnlyHint"`
	ConsequentialHint    bool `json:"consequentialHint"`
	UntrustedContentHint bool `json:"untrustedContentHint"`
}

func (WebMCP) Emit(
	ctx context.Context,
	document *tir.Document,
	options Options,
) (Result, error) {
	canonical, err := canonicalDocument(ctx, FormatWebMCP, document)
	if err != nil {
		return Result{}, err
	}
	definitions := make([]webMCPDefinition, 0, len(canonical.Tools))
	pageOrigin := webMCPPageOrigin(canonical.Source)
	for ti := range canonical.Tools {
		if err := canceled(ctx, FormatWebMCP); err != nil {
			return Result{}, err
		}
		tool := filterWebMCPTool(&canonical.Tools[ti], pageOrigin, canonical.FrameCoverage)
		if tool == nil {
			continue
		}
		for ai, action := range tool.Actions {
			if len(action.LocatorCandidateIDs) == 0 {
				return Result{}, failure(
					FormatWebMCP,
					CodeUnsupportedShape,
					fmt.Sprintf("tools[%q].actions[%d].locatorCandidateIds", tool.ID, ai),
					"executable WebMCP actions require at least one locator candidate",
				)
			}
		}
		inputSchema, err := toolInputSchema(ctx, FormatWebMCP, tool, options.Strict)
		if err != nil {
			return Result{}, err
		}
		definitions = append(definitions, webMCPDefinition{
			Name:        tool.ID,
			Description: tool.Description,
			InputSchema: inputSchema,
			Annotations: deriveWebMCPAnnotations(tool.Actions),
			Actions:     tool.Actions,
			Locators:    tool.Locators,
		})
	}
	definitionsJSON, err := json.Marshal(definitions)
	if err != nil {
		return Result{}, wrap(FormatWebMCP, CodeMarshal, "", err)
	}
	size := len(webMCPPrefix) + len(definitionsJSON) + len(webMCPDefinitionsEnd) +
		len(webMCPRuntime) + len(webMCPRegistration)
	data := make([]byte, 0, size)
	data = append(data, webMCPPrefix...)
	data = append(data, definitionsJSON...)
	data = append(data, webMCPDefinitionsEnd...)
	data = append(data, webMCPRuntime...)
	data = append(data, webMCPRegistration...)
	return Result{Primary: Artifact{
		Name:      "tools.webmcp.js",
		MediaType: "text/javascript",
		Data:      data,
	}}, nil
}

func webMCPPageOrigin(source tir.SourceMetadata) string {
	if origin := pageurl.Origin(source.FinalURL); origin != "" {
		return origin
	}
	return pageurl.Origin(source.RequestedURL)
}

func filterWebMCPTool(tool *tir.Tool, pageOrigin string, coverage tir.FrameCoverage) *tir.Tool {
	for _, action := range tool.Actions {
		if len(action.LocatorCandidateIDs) == 0 {
			return tool
		}
	}
	keep := make(map[string]tir.LocatorCandidate, len(tool.Locators))
	locators := make([]tir.LocatorCandidate, 0, len(tool.Locators))
	for _, locator := range tool.Locators {
		if !locatorResolvableInPage(locator, pageOrigin, coverage) {
			continue
		}
		keep[locator.ID] = locator
		locators = append(locators, locator)
	}
	if len(locators) == 0 {
		return nil
	}
	actions := make([]tir.ActionBinding, 0, len(tool.Actions))
	for _, action := range tool.Actions {
		ids := make([]string, 0, len(action.LocatorCandidateIDs))
		for _, id := range action.LocatorCandidateIDs {
			if _, ok := keep[id]; ok {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		action.LocatorCandidateIDs = ids
		actions = append(actions, action)
	}
	filtered := *tool
	filtered.Locators = locators
	filtered.Actions = actions
	return &filtered
}

func locatorResolvableInPage(locator tir.LocatorCandidate, pageOrigin string, coverage tir.FrameCoverage) bool {
	if len(locator.FramePath) == 0 {
		return true
	}
	if framePathUncovered(locator.FramePath, coverage) {
		return false
	}
	origin, src, covered := coverageOriginForFramePath(locator.FramePath, coverage)
	if !covered {
		return true
	}
	if pageOrigin != "" {
		if origin != "" && origin != pageOrigin {
			return false
		}
		if srcOrigin := pageurl.Origin(src); srcOrigin != "" && srcOrigin != pageOrigin {
			return false
		}
	}
	return true
}

func framePathUncovered(path []tir.PathNode, coverage tir.FrameCoverage) bool {
	indexes := framePathIndexes(path)
	for _, frame := range coverage.Uncovered {
		if frameIndexesMatch(frame.Path, indexes) {
			return true
		}
	}
	return false
}

func framePathIndexes(path []tir.PathNode) []int {
	indexes := make([]int, len(path))
	for i, node := range path {
		if node.Semantic != nil {
			indexes[i] = node.Semantic.Nth
		}
	}
	return indexes
}

func frameIndexesMatch(path []tir.FrameReference, indexes []int) bool {
	if len(path) != len(indexes) {
		return false
	}
	for i, node := range path {
		if node.Index != indexes[i] {
			return false
		}
	}
	return true
}

func coverageOriginForFramePath(path []tir.PathNode, coverage tir.FrameCoverage) (origin, src string, covered bool) {
	indexes := framePathIndexes(path)
	for _, frame := range coverage.Frames {
		if frameIndexesMatch(frame.Path, indexes) {
			src = ""
			if len(frame.Path) > 0 {
				src = frame.Path[len(frame.Path)-1].Src
			}
			return frame.Origin, src, true
		}
	}
	return "", "", false
}

func deriveWebMCPAnnotations(actions []tir.ActionBinding) webMCPAnnotations {
	hints := annotationHintsFrom(actions)
	return webMCPAnnotations{
		ReadOnlyHint:         hints.readOnly,
		ConsequentialHint:    hints.openWorld,
		UntrustedContentHint: true,
	}
}

const webMCPPrefix = `// Generated by GEO-Visor for the Chrome 153 WebMCP imperative API.
if (typeof document === "undefined" ||
    !document.modelContext ||
    typeof document.modelContext.registerTool !== "function") {
  throw new Error("GEO-Visor WebMCP requires document.modelContext.registerTool (Chrome 153 or compatible).");
}

const __geovisorDefinitions = `

// webMCPDefinitionsEnd terminates the definitions statement before the embedded
// runtime bundle, which begins with its own "use strict" directive.
const webMCPDefinitionsEnd = ";\n\n"

const webMCPRegistration = `
export const geovisorRegistrations = await __geovisorRuntime.register(__geovisorDefinitions);
`

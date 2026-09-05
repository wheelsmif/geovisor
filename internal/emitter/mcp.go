package emitter

import (
	"context"
	"encoding/json"

	"github.com/wheelsmif/geovisor/internal/tir"
)

// MCPSpecVersion is the protocol revision used by the static tools/list result.
const MCPSpecVersion = "2026-07-28"

// MCP emits a tools/list result object. It does not implement an MCP runtime.
type MCP struct{}

func (MCP) Format() Format {
	return FormatMCP
}

type mcpListToolsResult struct {
	Tools []mcpTool `json:"tools"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema *jsonSchema    `json:"inputSchema"`
	Annotations mcpAnnotations `json:"annotations"`
}

type mcpAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	IdempotentHint  bool `json:"idempotentHint"`
	OpenWorldHint   bool `json:"openWorldHint"`
}

func (MCP) Emit(
	ctx context.Context,
	document *tir.Document,
	_ Options,
) (Result, error) {
	canonical, err := canonicalDocument(ctx, FormatMCP, document)
	if err != nil {
		return Result{}, err
	}
	manifest := mcpListToolsResult{Tools: make([]mcpTool, 0, len(canonical.Tools))}
	for i := range canonical.Tools {
		if err := canceled(ctx, FormatMCP); err != nil {
			return Result{}, err
		}
		tool := &canonical.Tools[i]
		inputSchema, err := toolInputSchema(ctx, FormatMCP, tool, false)
		if err != nil {
			return Result{}, err
		}
		manifest.Tools = append(manifest.Tools, mcpTool{
			Name:        tool.ID,
			Title:       tool.Name,
			Description: tool.Description,
			InputSchema: inputSchema,
			Annotations: deriveMCPAnnotations(tool.Actions),
		})
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return Result{}, wrap(FormatMCP, CodeMarshal, "", err)
	}
	companion, err := bindingArtifactForCanonical(ctx, canonical, FormatMCP)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Primary: Artifact{
			Name:      "tools.mcp.json",
			MediaType: "application/json",
			Data:      data,
		},
		Companion: &companion,
	}, nil
}

func deriveMCPAnnotations(actions []tir.ActionBinding) mcpAnnotations {
	readOnly := true
	openWorld := false
	for _, action := range actions {
		if action.SideEffect.Class != tir.SideEffectNone {
			readOnly = false
		}
		switch action.SideEffect.Class {
		case tir.SideEffectNetwork, tir.SideEffectNavigation,
			tir.SideEffectSubmission, tir.SideEffectUnknown:
			openWorld = true
		}
	}
	return mcpAnnotations{
		ReadOnlyHint:    readOnly,
		DestructiveHint: !readOnly,
		IdempotentHint:  readOnly,
		OpenWorldHint:   openWorld,
	}
}

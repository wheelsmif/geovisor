package emitter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/geo-suite/geovisor/internal/tir"
)

// OpenAIToolShape identifies the selected unwrapped Responses API tool shape.
const OpenAIToolShape = "Responses API function tools, 2026-09-04 documentation"

// OpenAI emits an array of Responses API function tools.
type OpenAI struct{}

func (OpenAI) Format() Format {
	return FormatOpenAI
}

type openAIFunctionTool struct {
	Type        string      `json:"type"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  *jsonSchema `json:"parameters"`
	Strict      bool        `json:"strict"`
}

func (OpenAI) Emit(
	ctx context.Context,
	document *tir.Document,
	options Options,
) (Result, error) {
	canonical, err := canonicalDocument(ctx, FormatOpenAI, document)
	if err != nil {
		return Result{}, err
	}
	tools := make([]openAIFunctionTool, 0, len(canonical.Tools))
	for i := range canonical.Tools {
		if err := canceled(ctx, FormatOpenAI); err != nil {
			return Result{}, err
		}
		tool := &canonical.Tools[i]
		if err := validateOpenAIName(tool.ID); err != nil {
			return Result{}, wrap(
				FormatOpenAI,
				CodeInvalidName,
				fmt.Sprintf("tools[%d].id", i),
				err,
			)
		}
		inputSchema, err := toolInputSchema(ctx, FormatOpenAI, tool, options.Strict)
		if err != nil {
			return Result{}, err
		}
		tools = append(tools, openAIFunctionTool{
			Type:        "function",
			Name:        tool.ID,
			Description: tool.Description,
			Parameters:  inputSchema,
			Strict:      options.Strict,
		})
	}
	data, err := json.Marshal(tools)
	if err != nil {
		return Result{}, wrap(FormatOpenAI, CodeMarshal, "", err)
	}
	companion, err := bindingArtifactForCanonical(ctx, canonical, FormatOpenAI)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Primary: Artifact{
			Name:      "tools.openai.json",
			MediaType: "application/json",
			Data:      data,
		},
		Companion: &companion,
	}, nil
}

func validateOpenAIName(name string) error {
	if len(name) == 0 || len(name) > 64 {
		return fmt.Errorf("must contain between 1 and 64 ASCII characters")
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '_' || character == '-' {
			continue
		}
		return fmt.Errorf("must match ^[A-Za-z0-9_-]+$")
	}
	return nil
}

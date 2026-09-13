// Package emitter translates canonical TIR into deterministic target artifacts.
package emitter

import (
	"context"
	"fmt"

	"github.com/wheelsmif/geovisor/internal/tir"
)

// Format names an output lane.
type Format string

const (
	FormatTIRJSON Format = "tir-json"
	FormatWebMCP  Format = "webmcp"
	FormatMCP     Format = "mcp"
	FormatOpenAI  Format = "openai"
)

// Options controls target-specific representation choices.
type Options struct {
	Strict bool
}

// Artifact is one deterministic output file.
type Artifact struct {
	Name      string
	MediaType string
	Data      []byte
}

// Result contains a standard target payload and, where required, a separate
// GEO-Visor browser-binding companion.
type Result struct {
	Primary   Artifact
	Companion *Artifact
}

// Emitter is the small common interface implemented by every output lane.
type Emitter interface {
	Format() Format
	Emit(context.Context, *tir.Document, Options) (Result, error)
}

// Registry is an ordered, deterministic emitter registry.
type Registry struct {
	emitters []Emitter
}

// NewRegistry creates a registry and rejects nil or duplicate emitters.
func NewRegistry(emitters ...Emitter) (*Registry, error) {
	registry := &Registry{emitters: make([]Emitter, 0, len(emitters))}
	for i, candidate := range emitters {
		if candidate == nil {
			return nil, failure("", CodeRegistry, fmt.Sprintf("emitters[%d]", i), "emitter must not be nil")
		}
		for _, existing := range registry.emitters {
			if existing.Format() == candidate.Format() {
				return nil, failure(candidate.Format(), CodeRegistry, "", "duplicate emitter format")
			}
		}
		registry.emitters = append(registry.emitters, candidate)
	}
	return registry, nil
}

// DefaultRegistry returns all built-in emitters in stable display order.
func DefaultRegistry() *Registry {
	registry, err := NewRegistry(
		CanonicalJSON{},
		WebMCP{},
		MCP{},
		OpenAI{},
	)
	if err != nil {
		panic(err)
	}
	return registry
}

// Formats returns registered formats in registry order.
func (r *Registry) Formats() []Format {
	if r == nil {
		return []Format{}
	}
	formats := make([]Format, len(r.emitters))
	for i, item := range r.emitters {
		formats[i] = item.Format()
	}
	return formats
}

// Emit selects a registered emitter.
func (r *Registry) Emit(
	ctx context.Context,
	format Format,
	document *tir.Document,
	options Options,
) (Result, error) {
	if err := canceled(ctx, format); err != nil {
		return Result{}, err
	}
	if r != nil {
		for _, item := range r.emitters {
			if item.Format() == format {
				return item.Emit(ctx, document, options)
			}
		}
	}
	return Result{}, failure(format, CodeUnsupportedFormat, "", "format is not registered")
}

func canonicalDocument(ctx context.Context, format Format, document *tir.Document) (*tir.Document, error) {
	if err := canceled(ctx, format); err != nil {
		return nil, err
	}
	canonical, err := tir.Canonical(document)
	if err != nil {
		return nil, wrap(format, CodeInvalidDocument, "", err)
	}
	return canonical, nil
}

// annotationHints are the side-effect flags shared by MCP and WebMCP. The two
// formats name the open-world/consequential bit differently but compute it
// from the same classes (GV-023).
type annotationHints struct {
	readOnly  bool
	openWorld bool
}

func annotationHintsFrom(actions []tir.ActionBinding) annotationHints {
	hints := annotationHints{readOnly: true}
	for _, action := range actions {
		if mutatingAction(action.Action) || action.SideEffect.Class != tir.SideEffectNone {
			hints.readOnly = false
		}
		switch action.SideEffect.Class {
		case tir.SideEffectNetwork, tir.SideEffectNavigation,
			tir.SideEffectSubmission, tir.SideEffectUnknown:
			hints.openWorld = true
		}
	}
	return hints
}

func mutatingAction(kind tir.ActionKind) bool {
	switch kind {
	case tir.ActionClick, tir.ActionFill, tir.ActionCheck, tir.ActionSelect:
		return true
	default:
		return false
	}
}

func canceled(ctx context.Context, format Format) error {
	if ctx == nil {
		return failure(format, CodeCanceled, "", "context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return wrap(format, CodeCanceled, "", err)
	}
	return nil
}

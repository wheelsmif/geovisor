package emitter

import (
	"context"

	"github.com/wheelsmif/geovisor/internal/tir"
)

// CanonicalJSON delegates canonical output to tir.Marshal.
type CanonicalJSON struct{}

func (CanonicalJSON) Format() Format {
	return FormatTIRJSON
}

func (CanonicalJSON) Emit(
	ctx context.Context,
	document *tir.Document,
	_ Options,
) (Result, error) {
	if err := canceled(ctx, FormatTIRJSON); err != nil {
		return Result{}, err
	}
	data, err := tir.Marshal(document)
	if err != nil {
		return Result{}, wrap(FormatTIRJSON, CodeInvalidDocument, "", err)
	}
	return Result{Primary: Artifact{
		Name:      "tir.json",
		MediaType: "application/json",
		Data:      data,
	}}, nil
}

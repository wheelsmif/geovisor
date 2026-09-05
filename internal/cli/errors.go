package cli

import (
	"context"
	"errors"

	"github.com/geo-suite/geovisor/internal/browser"
	"github.com/geo-suite/geovisor/internal/emitter"
)

const (
	ExitSuccess  = 0
	ExitGeneric  = 1
	ExitUsage    = 2
	ExitBrowser  = 3
	ExitCompile  = 4
	ExitEmitter  = 5
	ExitOutput   = 6
	ExitCanceled = 130
)

// UsageError reports invalid command-line input.
type UsageError struct {
	Message string
}

func (e *UsageError) Error() string {
	return e.Message
}

type classifiedError struct {
	code int
	err  error
}

func (e *classifiedError) Error() string {
	return e.err.Error()
}

func (e *classifiedError) Unwrap() error {
	return e.err
}

func classify(code int, err error) error {
	if err == nil {
		return nil
	}
	return &classifiedError{code: code, err: err}
}

// ExitCode maps typed command errors to stable process exit codes.
func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	if errors.Is(err, context.Canceled) {
		return ExitCanceled
	}

	var browserErr *browser.Error
	if errors.As(err, &browserErr) && browserErr.Code == browser.ErrorCanceled {
		return ExitCanceled
	}
	if emitter.IsErrorCode(err, emitter.CodeCanceled) {
		return ExitCanceled
	}

	var usageErr *UsageError
	if errors.As(err, &usageErr) {
		return ExitUsage
	}

	var classified *classifiedError
	if errors.As(err, &classified) {
		return classified.code
	}
	return ExitGeneric
}

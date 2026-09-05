package emitter

import (
	"errors"
	"fmt"
)

// ErrorCode identifies a failure callers may handle without parsing text.
type ErrorCode string

const (
	CodeCanceled          ErrorCode = "canceled"
	CodeInvalidDocument   ErrorCode = "invalid_document"
	CodeUnsupportedFormat ErrorCode = "unsupported_format"
	CodeUnsupportedShape  ErrorCode = "unsupported_shape"
	CodeInvalidName       ErrorCode = "invalid_name"
	CodeMarshal           ErrorCode = "marshal_failed"
	CodeRegistry          ErrorCode = "registry_error"
)

// Error is a typed package-boundary error.
type Error struct {
	Format Format
	Code   ErrorCode
	Field  string
	Err    error
}

func (e *Error) Error() string {
	location := ""
	if e.Field != "" {
		location = " at " + e.Field
	}
	if e.Format == "" {
		return fmt.Sprintf("emitter %s%s: %v", e.Code, location, e.Err)
	}
	return fmt.Sprintf("%s emitter %s%s: %v", e.Format, e.Code, location, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

// IsErrorCode reports whether err is an emitter Error with code.
func IsErrorCode(err error, code ErrorCode) bool {
	var emitterErr *Error
	return errors.As(err, &emitterErr) && emitterErr.Code == code
}

func failure(format Format, code ErrorCode, field, message string) error {
	return &Error{Format: format, Code: code, Field: field, Err: errors.New(message)}
}

func wrap(format Format, code ErrorCode, field string, err error) error {
	return &Error{Format: format, Code: code, Field: field, Err: err}
}

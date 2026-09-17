package org

import (
	"errors"
	"fmt"
)

// ErrorKind is a machine readable category for domain errors. The transport
// layer maps it onto an HTTP status code.
type ErrorKind string

const (
	// KindValidation means the request payload is malformed or incomplete.
	KindValidation ErrorKind = "validation"
	// KindNotFound means the referenced entity does not exist.
	KindNotFound ErrorKind = "not_found"
	// KindConflict means the request clashes with existing state.
	KindConflict ErrorKind = "conflict"
	// KindInternal is the fallback for unexpected errors.
	KindInternal ErrorKind = "internal"
)

// Error is the typed error returned by the service layer.
type Error struct {
	Kind    ErrorKind
	Message string
}

// Error implements the error interface.
func (e *Error) Error() string { return e.Message }

// Validationf builds a validation error.
func Validationf(format string, args ...any) *Error {
	return &Error{Kind: KindValidation, Message: fmt.Sprintf(format, args...)}
}

// NotFoundf builds a not-found error.
func NotFoundf(format string, args ...any) *Error {
	return &Error{Kind: KindNotFound, Message: fmt.Sprintf(format, args...)}
}

// Conflictf builds a conflict error.
func Conflictf(format string, args ...any) *Error {
	return &Error{Kind: KindConflict, Message: fmt.Sprintf(format, args...)}
}

// Internalf builds an internal error, used when a mutation cannot be persisted.
func Internalf(format string, args ...any) *Error {
	return &Error{Kind: KindInternal, Message: fmt.Sprintf(format, args...)}
}

// KindOf extracts the kind of a typed domain error. Unknown errors are
// reported as KindInternal.
func KindOf(err error) ErrorKind {
	var e *Error
	if errors.As(err, &e) && e.Kind != "" {
		return e.Kind
	}
	return KindInternal
}

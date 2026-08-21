package model

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrCode is a stable machine-readable error code.
type ErrCode string

const (
	CodeNotFound      ErrCode = "NOT_FOUND"
	CodeConflict       ErrCode = "CONFLICT"
	CodeStateConflict  ErrCode = "STATE_CONFLICT"
	CodeInvariant      ErrCode = "INVARIANT"
	CodePointOccupied  ErrCode = "POINT_OCCUPIED"
	CodeTimeout        ErrCode = "TIMEOUT"
	CodeInternal       ErrCode = "INTERNAL"
)

// Error is the engine's standard API error. It carries an HTTP status and a
// stable code so handlers can render a uniform {"error","code"} body.
type Error struct {
	Status  int
	Code    ErrCode
	Message string
}

func (e *Error) Error() string { return e.Message }

// NewError builds a standard error.
func NewError(status int, code ErrCode, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

// NotFoundf is a 404 helper.
func NotFoundf(format string, args ...any) *Error {
	return NewError(http.StatusNotFound, CodeNotFound, fmt.Sprintf(format, args...))
}

// Conflictf is a 409 helper for layout/locking conflicts.
func Conflictf(format string, args ...any) *Error {
	return NewError(http.StatusConflict, CodeConflict, fmt.Sprintf(format, args...))
}

// StateConflictf is a 409 helper for illegal state-machine transitions.
func StateConflictf(format string, args ...any) *Error {
	return NewError(http.StatusConflict, CodeStateConflict, fmt.Sprintf(format, args...))
}

// Invariantf is a 422 helper for validation failures (empty/duplicate/out-of-range).
func Invariantf(format string, args ...any) *Error {
	return NewError(http.StatusUnprocessableEntity, CodeInvariant, fmt.Sprintf(format, args...))
}

// PointOccupiedf is a 409 helper for moving a point under a train.
func PointOccupiedf(format string, args ...any) *Error {
	return NewError(http.StatusConflict, CodePointOccupied, fmt.Sprintf(format, args...))
}

// Timeoutf is a 408 helper for switch move timeout / out of correspondence.
func Timeoutf(format string, args ...any) *Error {
	return NewError(http.StatusRequestTimeout, CodeTimeout, fmt.Sprintf(format, args...))
}

// AsError unwraps an error into *Error, defaulting to a 500 internal error.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var se *Error
	if errors.As(err, &se) {
		return se
	}
	return NewError(http.StatusInternalServerError, CodeInternal, err.Error())
}

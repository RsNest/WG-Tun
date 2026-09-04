package model

import (
	"errors"
	"fmt"
	"net/http"
)

// CodedError is an API/domain error with a stable machine-readable code.
type CodedError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	HTTP    int    `json:"-"`
}

func (e *CodedError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func Validation(msg string) *CodedError {
	return &CodedError{Code: "VALIDATION", Message: msg, HTTP: http.StatusBadRequest}
}

func NotFound(resource, id string) *CodedError {
	return &CodedError{Code: "NOT_FOUND", Message: fmt.Sprintf("%s %s not found", resource, id), HTTP: http.StatusNotFound}
}

func ErrConflict(msg string) *CodedError {
	return &CodedError{Code: "CONFLICT", Message: msg, HTTP: http.StatusConflict}
}

func Unauthorized(msg string) *CodedError {
	return &CodedError{Code: "UNAUTHORIZED", Message: msg, HTTP: http.StatusUnauthorized}
}

func Forbidden(msg string) *CodedError {
	return &CodedError{Code: "FORBIDDEN", Message: msg, HTTP: http.StatusForbidden}
}

func Unavailable(msg string) *CodedError {
	return &CodedError{Code: "UNAVAILABLE", Message: msg, HTTP: http.StatusServiceUnavailable}
}

func Internal(msg string) *CodedError {
	return &CodedError{Code: "INTERNAL", Message: msg, HTTP: http.StatusInternalServerError}
}

func NotImplemented(msg string) *CodedError {
	return &CodedError{Code: "NOT_IMPLEMENTED", Message: msg, HTTP: http.StatusNotImplemented}
}

func AsCoded(err error) *CodedError {
	var ce *CodedError
	if errors.As(err, &ce) {
		return ce
	}
	return Internal(err.Error())
}

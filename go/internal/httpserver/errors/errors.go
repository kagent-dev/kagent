package errors

import (
	"fmt"
	"net/http"
)

// APIError represents an API error with HTTP status code and message
type APIError struct {
	Code    int
	Message string
	Err     error
}

// Error implements the error interface
func (e *APIError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap returns the wrapped error
func (e *APIError) Unwrap() error {
	return e.Err
}

// StatusCode returns the HTTP status code
func (e *APIError) StatusCode() int {
	return e.Code
}

// NewBadRequestError creates a new bad request error
func NewBadRequestError(message string, err error) *APIError {
	return &APIError{
		Code:    http.StatusBadRequest,
		Message: message,
		Err:     err,
	}
}

// NewNotFoundError creates a new not found error
func NewNotFoundError(message string, err error) *APIError {
	return &APIError{
		Code:    http.StatusNotFound,
		Message: message,
		Err:     err,
	}
}

// NewInternalServerError creates a new internal server error
func NewInternalServerError(message string, err error) *APIError {
	return &APIError{
		Code:    http.StatusInternalServerError,
		Message: message,
		Err:     err,
	}
}

// NewValidationError creates a new validation error
func NewValidationError(message string, err error) *APIError {
	return &APIError{
		Code:    http.StatusUnprocessableEntity,
		Message: message,
		Err:     err,
	}
}

func NewConflictError(message string, err error) *APIError {
	return &APIError{
		Code:    http.StatusConflict,
		Message: message,
		Err:     err,
	}
}

func NewNotImplementedError(message string, err error) *APIError {
	return &APIError{
		Code:    http.StatusNotImplemented,
		Message: message,
		Err:     err,
	}
}

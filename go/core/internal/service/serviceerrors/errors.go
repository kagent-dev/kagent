package serviceerrors

import "errors"

type Code string

const (
	CodeInvalidArgument    Code = "invalid_argument"
	CodeUnauthenticated    Code = "unauthenticated"
	CodePermissionDenied   Code = "permission_denied"
	CodeNotFound           Code = "not_found"
	CodeAlreadyExists      Code = "already_exists"
	CodeFailedPrecondition Code = "failed_precondition"
	CodeResourceExhausted  Code = "resource_exhausted"
	CodeUnavailable        Code = "unavailable"
	CodeInternal           Code = "internal"
)

type Error struct {
	code    Code
	message string
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}

func (e *Error) Unwrap() error {
	return e.cause
}

func (e *Error) Code() Code {
	if e == nil {
		return ""
	}
	return e.code
}

func (e *Error) Message() string {
	if e == nil {
		return ""
	}
	return e.message
}

func New(code Code, message string, cause error) *Error {
	return &Error{code: code, message: message, cause: cause}
}

func NewInvalidArgument(message string, cause error) *Error {
	return New(CodeInvalidArgument, message, cause)
}

func NewUnauthenticated(message string, cause error) *Error {
	return New(CodeUnauthenticated, message, cause)
}

func NewPermissionDenied(message string, cause error) *Error {
	return New(CodePermissionDenied, message, cause)
}

func NewNotFound(message string, cause error) *Error {
	return New(CodeNotFound, message, cause)
}

func NewAlreadyExists(message string, cause error) *Error {
	return New(CodeAlreadyExists, message, cause)
}

func NewFailedPrecondition(message string, cause error) *Error {
	return New(CodeFailedPrecondition, message, cause)
}

func NewResourceExhausted(message string, cause error) *Error {
	return New(CodeResourceExhausted, message, cause)
}

func NewUnavailable(message string, cause error) *Error {
	return New(CodeUnavailable, message, cause)
}

func NewInternal(message string, cause error) *Error {
	return New(CodeInternal, message, cause)
}

func CodeOf(err error) Code {
	var serviceError *Error
	if errors.As(err, &serviceError) {
		return serviceError.Code()
	}
	return ""
}

func MessageOf(err error) string {
	var serviceError *Error
	if errors.As(err, &serviceError) {
		return serviceError.Message()
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

func IsCode(err error, code Code) bool {
	return CodeOf(err) == code
}

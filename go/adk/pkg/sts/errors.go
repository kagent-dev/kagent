package sts

import "fmt"

// STSError is the base error type for STS client errors.
type STSError struct {
	Message string
}

func (e *STSError) Error() string {
	return e.Message
}

// TokenExchangeError is raised when token exchange fails.
type TokenExchangeError struct {
	STSError
	ErrorCode        string
	ErrorDescription string
	StatusCode       int
}

func (e *TokenExchangeError) Error() string {
	if e.ErrorDescription != "" {
		return fmt.Sprintf("token exchange failed: %s - %s (status: %d)", e.ErrorCode, e.ErrorDescription, e.StatusCode)
	}
	return fmt.Sprintf("token exchange failed: %s (status: %d)", e.ErrorCode, e.StatusCode)
}

// NewTokenExchangeError creates a new TokenExchangeError.
func NewTokenExchangeError(errorCode, errorDescription string, statusCode int) *TokenExchangeError {
	return &TokenExchangeError{
		STSError:         STSError{Message: fmt.Sprintf("token exchange failed: %s", errorCode)},
		ErrorCode:        errorCode,
		ErrorDescription: errorDescription,
		StatusCode:       statusCode,
	}
}

// ConfigurationError is raised when STS configuration is invalid.
type ConfigurationError struct {
	STSError
}

// NewConfigurationError creates a new ConfigurationError.
func NewConfigurationError(message string) *ConfigurationError {
	return &ConfigurationError{
		STSError: STSError{Message: fmt.Sprintf("STS configuration error: %s", message)},
	}
}

// AuthenticationError is raised when authentication fails.
type AuthenticationError struct {
	STSError
}

// NewAuthenticationError creates a new AuthenticationError.
func NewAuthenticationError(message string) *AuthenticationError {
	return &AuthenticationError{
		STSError: STSError{Message: fmt.Sprintf("STS authentication error: %s", message)},
	}
}

// NetworkError is raised when network operations fail.
type NetworkError struct {
	STSError
	Cause error
}

func (e *NetworkError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("STS network error: %s: %v", e.Message, e.Cause)
	}
	return fmt.Sprintf("STS network error: %s", e.Message)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *NetworkError) Unwrap() error {
	return e.Cause
}

// NewNetworkError creates a new NetworkError.
func NewNetworkError(message string, cause error) *NetworkError {
	return &NetworkError{
		STSError: STSError{Message: message},
		Cause:    cause,
	}
}

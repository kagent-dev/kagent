package common

import (
	"fmt"
	"strings"
)

// SupportedFrameworks lists all available frameworks
var SupportedFrameworks = []string{"adk", "crewai", "langgraph"}

// FrameworkValidator validates framework selections
type FrameworkValidator struct{}

// NewFrameworkValidator creates a new validator
func NewFrameworkValidator() *FrameworkValidator {
	return &FrameworkValidator{}
}

// Validate checks if the framework is supported
func (v *FrameworkValidator) Validate(framework string) error {
	framework = strings.ToLower(framework)
	for _, supported := range SupportedFrameworks {
		if framework == supported {
			return nil
		}
	}
	return fmt.Errorf("unsupported framework '%s'. Supported frameworks: %s",
		framework, strings.Join(SupportedFrameworks, ", "))
}

// GetSupportedFrameworks returns the list of supported frameworks
func (v *FrameworkValidator) GetSupportedFrameworks() []string {
	return SupportedFrameworks
}

// IsSupported checks if a framework is supported
func (v *FrameworkValidator) IsSupported(framework string) bool {
	return v.Validate(framework) == nil
}

package utils

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectWithModelConfig represents a Kubernetes resource that can be associated with a ModelConfig.
// It extends client.Object to provide access to standard Kubernetes object metadata
// while adding the ability to specify which ModelConfig should be used for the resource.
// Implementers must provide a GetModelConfigName() method that returns either:
// - An empty string: indicating the default ModelConfig should be used
// - A name: indicating a ModelConfig in the same namespace as the resource
// - A namespace/name reference: indicating a specific ModelConfig in a specific namespace
type ObjectWithModelConfig interface {
	client.Object
	GetModelConfigName() string
}

// GetResourceNamespace returns the namespace for resources,
// using the KAGENT_NAMESPACE environment variable or defaulting to "kagent".
func GetResourceNamespace() string {
	if val := os.Getenv("KAGENT_NAMESPACE"); val != "" {
		return val
	}
	return "kagent"
}

// GetControllerName returns the name for the kagent controller,
// using the KAGENT_CONTROLLER_NAME environment variable or defaulting to "kagent-controller".
func GetControllerName() string {
	if val := os.Getenv("KAGENT_CONTROLLER_NAME"); val != "" {
		return val
	}
	return "kagent-controller"
}

// ResourceRefString formats namespace and name as a string reference in "namespace/name" format.
func ResourceRefString(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// GetObjectRef formats a Kubernetes object reference as "namespace/name" string.
func GetObjectRef(obj client.Object) string {
	return ResourceRefString(obj.GetNamespace(), obj.GetName())
}

// containsWhitespace reports whether s contains any Unicode whitespace characters.
func containsWhitespace(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

// validateDNS1123Subdomain validates a DNS1123 subdomain and returns a descriptive error
func validateDNS1123Subdomain(value, fieldName string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", fieldName)
	}

	// For comprehensive log messages
	if containsWhitespace(value) {
		return fmt.Errorf("%s cannot contain whitespace characters: %q", fieldName, value)
	}

	if errs := validation.IsDNS1123Subdomain(value); len(errs) > 0 {
		return fmt.Errorf("invalid %s %s: %v", fieldName, value, strings.Join(errs, ", "))
	}

	return nil
}

type EmptyReferenceError struct{}

func (e *EmptyReferenceError) Error() string {
	return "empty reference string"
}

// ParseRefString parses a string reference (either "namespace/name" or just "name")
// into a NamespacedName object, using parentNamespace when namespace is not specified.
func ParseRefString(ref string, parentNamespace string) (types.NamespacedName, error) {
	if ref == "" {
		return types.NamespacedName{}, &EmptyReferenceError{}
	}

	slashCount := strings.Count(ref, "/")

	// Too many slashes in ref
	if slashCount > 1 {
		return types.NamespacedName{}, fmt.Errorf("reference cannot contain more than one slash")
	}

	// ref contains only name
	if slashCount == 0 {
		if parentNamespace == "" {
			return types.NamespacedName{}, fmt.Errorf("parent namespace cannot be empty when reference doesn't contain namespace")
		}

		if err := validateDNS1123Subdomain(ref, "name"); err != nil {
			return types.NamespacedName{}, err
		}

		return types.NamespacedName{
			Namespace: parentNamespace,
			Name:      ref,
		}, nil
	}

	// ref is in namespace/name format
	slashIndex := strings.Index(ref, "/")
	namespace := ref[:slashIndex]
	name := ref[slashIndex+1:]

	if namespace == "" && name == "" {
		return types.NamespacedName{}, fmt.Errorf("namespace and name cannot be empty")
	}

	if namespace == "" {
		return types.NamespacedName{}, fmt.Errorf("namespace cannot be empty")
	}

	if name == "" {
		return types.NamespacedName{}, fmt.Errorf("name cannot be empty")
	}

	if err := validateDNS1123Subdomain(namespace, "namespace"); err != nil {
		return types.NamespacedName{}, err
	}

	if err := validateDNS1123Subdomain(name, "name"); err != nil {
		return types.NamespacedName{}, err
	}

	return types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, nil
}

// ConvertToPythonIdentifier converts Kubernetes identifiers to Python-compatible format
// by replacing hyphens with underscores and slashes with "__NS__".
func ConvertToPythonIdentifier(name string) string {
	name = strings.ReplaceAll(name, "-", "_")
	return strings.ReplaceAll(name, "/", "__NS__") // RFC 1123 will guarantee there will be no conflicts
}

// ConvertToKubernetesIdentifier converts Python identifiers back to Kubernetes format
// by replacing "__NS__" with slashes and underscores with hyphens.
func ConvertToKubernetesIdentifier(name string) string {
	name = strings.ReplaceAll(name, "__NS__", "/")
	return strings.ReplaceAll(name, "_", "-")
}

// ParseStringToFloat64 parses a string to float64, returns nil if empty or invalid
func ParseStringToFloat64(s string) *float64 {
	if s == "" {
		return nil
	}
	if val, err := strconv.ParseFloat(s, 64); err == nil {
		return &val
	}
	return nil
}

package common

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
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

func GetGlobalUserID() string {
	if val := os.Getenv("KAGENT_GLOBAL_USER_ID"); val != "" {
		return val
	}
	return "admin@kagent.dev"
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

// GetModelConfig retrieves the ModelConfig for a resource.
// It uses the resource's specified model config name or falls back to the default.
func GetModelConfig(
	ctx context.Context,
	kube client.Client,
	resource ObjectWithModelConfig,
	defaultModelConfig types.NamespacedName,
) (*v1alpha1.ModelConfig, error) {
	// Start with the default model config reference
	modelConfigRef := defaultModelConfig

	// If the resource specifies a model config, parse and use that reference instead
	if modelConfigName := resource.GetModelConfigName(); modelConfigName != "" {
		// Parse the model config name, which could be just a name or namespace/name
		// If just a name, use the resource's namespace as the parent namespace
		if parsedRef, err := ParseRefString(modelConfigName, resource.GetNamespace()); err != nil {
			return nil, err
		} else {
			modelConfigRef = parsedRef
		}
	} else {
		// Log (DEBUG) that we're using the default model config when none is specified
		ctrllog.FromContext(ctx).V(4).Info("Using default ModelConfig",
			"kind", resource.GetObjectKind().GroupVersionKind().Kind,
			"namespace", resource.GetNamespace(),
			"name", resource.GetName(),
			"modelconfig", modelConfigRef.String(),
		)
	}

	// Fetch the model config object from the Kubernetes API
	modelConfigObj := &v1alpha1.ModelConfig{}
	if err := GetObject(
		ctx,
		kube,
		modelConfigObj,
		modelConfigRef.Name,
		modelConfigRef.Namespace,
	); err != nil {
		return nil, fmt.Errorf(
			"failed to fetch ModelConfig %s: %w", modelConfigRef.String(), err,
		)
	}
	return modelConfigObj, nil
}

// GetObject fetches the Kubernetes resource identified by objRef into obj.
// objRef may be given as "namespace/name" or just "name"; if the namespace is missing, defaultNamespace is applied
func GetObject(ctx context.Context, kube client.Client, obj client.Object, objRef, defaultNamespace string) error {
	ref, err := ParseRefString(objRef, defaultNamespace)
	if err != nil {
		return err
	}

	err = kube.Get(ctx, ref, obj)
	if err != nil {
		return err
	}

	return nil
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

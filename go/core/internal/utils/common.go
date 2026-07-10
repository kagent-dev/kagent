package utils

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
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
	return env.KagentNamespace.Get()
}

// GetControllerName returns the name for the kagent controller,
// using the KAGENT_CONTROLLER_NAME environment variable or defaulting to "kagent-controller".
func GetControllerName() string {
	return env.KagentControllerName.Get()
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
	namespace, name, _ := strings.Cut(ref, "/")

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

// Agent-like kinds sharing the agent DB table. Values match the CRD kind names.
const (
	AgentKind        = "Agent"
	SandboxAgentKind = "SandboxAgent"
	AgentHarnessKind = "AgentHarness"
)

// GroupKind strings API routes accept to select a kind (kind.group of the
// kagent.dev CRDs). Absent always means Agent; which values a route accepts
// varies (the /api/agents routes serve Agent/SandboxAgent only, the sessions
// API additionally accepts AgentHarness).
const (
	AgentGroupKind        = AgentKind + ".kagent.dev"
	SandboxAgentGroupKind = SandboxAgentKind + ".kagent.dev"
	AgentHarnessGroupKind = AgentHarnessKind + ".kagent.dev"
)

// Kind prefixes folded into the DB id for the experimental kinds. They mirror
// the API route resources (/api/sandboxagents, /api/agentharnesses) and contain
// no '-' or '_' so the python/kubernetes identifier conversions leave them intact.
const (
	sandboxAgentIDPrefix = "sandboxagents/"
	agentHarnessIDPrefix = "agentharnesses/"
)

// AgentDBID returns the identity of an agent-like resource in the agent DB
// table (and in session.agent_id). Agent rows keep the historical bare
// ConvertToPythonIdentifier("ns/name"); SandboxAgent and AgentHarness rows are
// kind-qualified so a same-named resource of another kind occupies a distinct
// row. ref is "namespace/name"; kind is one of the *Kind constants (anything
// else maps to the bare Agent format).
func AgentDBID(kind, ref string) string {
	switch kind {
	case SandboxAgentKind:
		return ConvertToPythonIdentifier(sandboxAgentIDPrefix + ref)
	case AgentHarnessKind:
		return ConvertToPythonIdentifier(agentHarnessIDPrefix + ref)
	default:
		return ConvertToPythonIdentifier(ref)
	}
}

// ParseAgentDBID returns the kind and "namespace/name" ref encoded in an agent
// DB id produced by AgentDBID (bare ids parse as Agent). A qualified ref always
// contains a "/" after the prefix is stripped; without one the prefix match was
// coincidental (an Agent in a namespace literally named "sandboxagents").
func ParseAgentDBID(id string) (kind, ref string) {
	k8s := ConvertToKubernetesIdentifier(id)
	if rest, ok := strings.CutPrefix(k8s, sandboxAgentIDPrefix); ok && strings.Contains(rest, "/") {
		return SandboxAgentKind, rest
	}
	if rest, ok := strings.CutPrefix(k8s, agentHarnessIDPrefix); ok && strings.Contains(rest, "/") {
		return AgentHarnessKind, rest
	}
	return AgentKind, k8s
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

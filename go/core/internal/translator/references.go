package translator

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// NamespaceObservation records either current metadata or a failed observation.
// Errors invalidate previous labels rather than leaving stale authorization.
type NamespaceObservation struct {
	Name      string
	Namespace *corev1.Namespace
	Error     string
}

func (n NamespaceObservation) ResourceName() string { return n.Name }

func authorizeReference(ctx krt.HandlerContext, collections Collections, source string, target types.NamespacedName, policy *v1alpha3.AllowedNamespaces) error {
	var namespace *corev1.Namespace
	if policy != nil && policy.From == v1alpha3.NamespacesFromSelector && collections.Namespaces != nil {
		observed := krt.FetchOne(ctx, collections.Namespaces, krt.FilterKey(source))
		if observed != nil {
			if observed.Error != "" {
				return fmt.Errorf("observe source namespace %q for allowedNamespaces.from=Selector: %s", source, observed.Error)
			}
			namespace = observed.Namespace
		}
	}
	allowed, err := policy.AllowsNamespaceMetadata(source, target.Namespace, namespace)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("reference from namespace %q denied by allowedNamespaces", source)
	}
	return nil
}

// ResolveModelReference authorizes a model before exposing dependent configuration
// errors. Target validity remains independent of any particular consumer.
func ResolveModelReference(ctx krt.HandlerContext, collections Collections, source string, ref v1alpha3.ModelConfigReference) (*ResolvedModelConfig, error) {
	key, model, err := fetchModel(ctx, collections, source, ref)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, fmt.Errorf("resolve ModelConfig %q: not found", key)
	}
	if err := authorizeReference(ctx, collections, source, key, model.Config.Spec.AllowedNamespaces); err != nil {
		return nil, fmt.Errorf("resolve ModelConfig %q: %w", key, err)
	}
	if failures := model.SemanticFailures; len(failures) > 0 {
		return nil, NewValidationError("ModelConfig %q: %s", key, failures[0].Message)
	}
	if failures := model.ReferenceFailures; len(failures) > 0 {
		return nil, fmt.Errorf("resolve ModelConfig %q: %s", key, failures[0].Message)
	}
	return model, nil
}

// fetchModel and fetchMCPServer hold the one kind switch per reference type.
// A nil object means not found; unregistered kinds fail with ErrInvalidKind.
func fetchModel(ctx krt.HandlerContext, collections Collections, source string, ref v1alpha3.ModelConfigReference) (types.NamespacedName, *ResolvedModelConfig, error) {
	switch kind, key := ref.Target(source); kind {
	case v1alpha3.ModelConfigGroupKind:
		return key, krt.FetchOne(ctx, collections.ResolvedModelConfigs, krt.FilterObjectName(key)), nil
	default:
		if models, ok := collections.ModelConfigKinds[kind]; ok {
			model := krt.FetchOne(ctx, models, krt.FilterObjectName(key))
			if model == nil {
				return key, nil, nil
			}
			resolved, err := ResolveModelConfig(ctx, collections, *model)
			return key, resolved, err
		}
		return key, nil, fmt.Errorf("resolve model %q: %w %s", key, ErrInvalidKind, kind)
	}
}

func fetchMCPServer(ctx krt.HandlerContext, collections Collections, source string, ref v1alpha3.MCPServerReference) (types.NamespacedName, *v1alpha3.RemoteMCPServer, error) {
	switch kind, key := ref.Target(source); kind {
	case v1alpha3.RemoteMCPServerGroupKind:
		return key, ptr.Deref(krt.FetchOne(ctx, collections.RemoteMCPServers, krt.FilterObjectName(key)), nil), nil
	default:
		if servers, ok := collections.MCPServerKinds[kind]; ok {
			return key, ptr.Deref(krt.FetchOne(ctx, servers, krt.FilterObjectName(key)), nil), nil
		}
		return key, nil, fmt.Errorf("resolve MCP server %q: %w %s", key, ErrInvalidKind, kind)
	}
}

// SourceKind returns a referenced object's kind: the TypeMeta a registered
// kind's projection sets, or the built-in kind.
func SourceKind(object runtime.Object, builtin string) schema.GroupVersionKind {
	if kind := object.GetObjectKind().GroupVersionKind(); !kind.Empty() {
		return kind
	}
	return v1alpha3.GroupVersion.WithKind(builtin)
}

// SameModelCredentials distinguishes target-local Secrets even when specs match.
func SameModelCredentials(a, b *v1alpha3.ModelConfig) bool {
	return a.Spec.APIKeySecret == b.Spec.APIKeySecret && a.Spec.APIKeySecretKey == b.Spec.APIKeySecretKey && (a.Spec.APIKeySecret == "" || a.Namespace == b.Namespace)
}

// MCPRuntimeName retains existing local names and gives shared servers bounded,
// namespace-qualified native names. Native adapters reject colliding bindings.
func MCPRuntimeName(source string, server *v1alpha3.RemoteMCPServer) string {
	name := strings.ReplaceAll(server.Name, ".", "_")
	if server.Namespace == source {
		return name
	}
	sum := sha256.Sum256([]byte(server.Namespace + "/" + server.Name))
	prefix := server.Namespace + "_" + name
	if len(prefix) > 45 {
		prefix = prefix[:45]
	}
	return fmt.Sprintf("%s_%x", prefix, sum[:8])
}

// ValidateSecretKey tracks target-local credentials without returning their values.
func ValidateSecretKey(ctx krt.HandlerContext, collections Collections, namespace, name, key string) error {
	ref := types.NamespacedName{Namespace: namespace, Name: name}
	secret := krt.FetchOne(ctx, collections.Secrets, krt.FilterObjectName(ref))
	if secret == nil {
		return fmt.Errorf("secret %q not found", ref)
	}
	if _, found := (*secret).Data[key]; !found {
		return fmt.Errorf("secret %q does not contain key %q", ref, key)
	}
	return nil
}

package handlers

import (
	"context"
	stderrors "errors"
	"fmt"
	"maps"
	"strings"

	"github.com/go-logr/logr"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// errInvalidCompanionSecret tags errors that should surface as a 400 Bad
// Request to the API caller (e.g. an existing Secret has the wrong type
// or is owned by a different resource).
var errInvalidCompanionSecret = stderrors.New("invalid companion secret")

// companionSecretAPIError translates a companion-Secret error into the
// appropriate HTTP API error. errInvalidCompanionSecret indicates
// caller-fixable conditions and surfaces as 400; anything else is an
// internal failure (kube API error, marshal failure, etc.).
func companionSecretAPIError(err error) *errors.APIError {
	if stderrors.Is(err, errInvalidCompanionSecret) {
		return errors.NewBadRequestError(err.Error(), err)
	}
	return errors.NewInternalServerError("Failed to create or update companion secrets", err)
}

// rollbackOwnerOnCompanionSecretFailure deletes the owner resource the
// caller just created when the companion-Secret pass that follows fails.
// Use it to close the partial-failure window where the owner is in K8s
// but its referenced Secrets aren't — the operator's retry of the same
// POST would otherwise hit AlreadyExists on the owner without realizing
// the prior attempt half-succeeded. Best-effort: a delete failure is
// logged via the caller's logger but does not change the outer error;
// the caller already surfaces the companion-Secret error to the client.
func rollbackOwnerOnCompanionSecretFailure(ctx context.Context, kubeClient client.Client, owner client.Object, log logr.Logger) {
	if err := kubeClient.Delete(ctx, owner); err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "failed to roll back owner after companion-secret failure",
			"kind", owner.GetObjectKind().GroupVersionKind().Kind,
			"namespace", owner.GetNamespace(),
			"name", owner.GetName())
	}
}

// validateSecretMaterials checks each SecretMaterial's name and key
// against Kubernetes naming rules. Returns a single error for the first
// invalid material so the caller can return a 400 with a precise reason.
func validateSecretMaterials(secrets []api.SecretMaterial) error {
	for _, secret := range secrets {
		if errs := validation.IsDNS1123Subdomain(secret.Name); len(errs) > 0 {
			return fmt.Errorf("invalid secret name %q: %s", secret.Name, strings.Join(errs, "; "))
		}
		if errs := validation.IsConfigMapKey(secret.Key); len(errs) > 0 {
			return fmt.Errorf("invalid key %q for secret %q: %s", secret.Key, secret.Name, strings.Join(errs, "; "))
		}
	}
	return nil
}

// createOrUpdateCompanionSecrets writes each SecretMaterial as an Opaque
// Secret in the owner's namespace, with an OwnerReference back to the
// owner so K8s garbage collection cleans them up when the parent is
// deleted. Materials grouped by `name` accumulate keys into a single
// Secret object.
//
// When a Secret with the same name already exists, the helper merges
// the new keys into the existing Data. Two safety checks:
//   - the existing Secret's type must be Opaque (mismatched types are a
//     400-class error because the caller's payload is incompatible).
//   - the existing Secret must already carry an OwnerReference back to
//     this owner (a Secret managed by someone else is not safe to
//     mutate from a different parent's create/update path).
//
// Caller is responsible for ensuring the owner has been written to the
// API server first (so .GetUID() is populated) — the OwnerReference
// uses the owner's live UID.
func createOrUpdateCompanionSecrets(
	ctx context.Context,
	kubeClient client.Client,
	owner client.Object,
	gvk schema.GroupVersionKind,
	secrets []api.SecretMaterial,
) error {
	// Group secrets by name and key.
	secretsByName := map[string]map[string][]byte{}
	for _, secret := range secrets {
		if _, ok := secretsByName[secret.Name]; !ok {
			secretsByName[secret.Name] = map[string][]byte{}
		}
		secretsByName[secret.Name][secret.Key] = []byte(secret.Value)
	}

	namespace := owner.GetNamespace()
	for name, data := range secretsByName {
		existingSecret := &corev1.Secret{}
		err := kubeClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, existingSecret)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to get companion secret %s/%s: %w", namespace, name, err)
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            name,
					Namespace:       namespace,
					OwnerReferences: []metav1.OwnerReference{ownerReferenceFor(owner, gvk)},
				},
				Type: corev1.SecretTypeOpaque,
				Data: data,
			}
			if err := kubeClient.Create(ctx, secret); err != nil {
				return fmt.Errorf("failed to create companion secret %s/%s: %w", namespace, name, err)
			}
			continue
		}

		if existingSecret.Type != corev1.SecretTypeOpaque {
			return fmt.Errorf("%w: companion secret %s/%s must be type %q, got %q", errInvalidCompanionSecret, namespace, name, corev1.SecretTypeOpaque, existingSecret.Type)
		}
		if !isOwnedBy(existingSecret, owner, gvk) {
			return fmt.Errorf("%w: companion secret %s/%s is not managed by %s %s/%s", errInvalidCompanionSecret, namespace, name, gvk.Kind, owner.GetNamespace(), owner.GetName())
		}

		if existingSecret.Data == nil {
			existingSecret.Data = map[string][]byte{}
		}
		maps.Copy(existingSecret.Data, data)
		if err := kubeClient.Update(ctx, existingSecret); err != nil {
			return fmt.Errorf("failed to update companion secret %s/%s: %w", namespace, name, err)
		}
	}

	return nil
}

// ownerReferenceFor returns an OwnerReference pointing at the given
// owner. GVK is taken explicitly rather than from owner's TypeMeta
// because objects roundtripped through client.Get often have empty
// TypeMeta; the caller knows the kind at the call site.
func ownerReferenceFor(owner client.Object, gvk schema.GroupVersionKind) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		APIVersion: gvk.GroupVersion().Identifier(),
		Kind:       gvk.Kind,
		Name:       owner.GetName(),
		UID:        owner.GetUID(),
		Controller: &controller,
	}
}

// isOwnedBy reports whether the secret carries an OwnerReference back
// to the given owner. Compares APIVersion, Kind, Name, AND UID — the UID
// match is load-bearing because a delete-recreate of an owner with the
// same name issues a fresh UID, and the prior owner's secrets may still
// be visible through K8s GC's deletion delay. Requires a non-empty UID
// on the caller's owner; in production K8s always populates this after
// Create, so a zero UID here means a programming error (calling the
// helper before persisting the owner).
func isOwnedBy(secret *corev1.Secret, owner client.Object, gvk schema.GroupVersionKind) bool {
	if owner.GetUID() == "" {
		return false
	}
	for _, ownerRef := range secret.GetOwnerReferences() {
		if ownerRef.APIVersion != gvk.GroupVersion().Identifier() ||
			ownerRef.Kind != gvk.Kind ||
			ownerRef.Name != owner.GetName() {
			continue
		}
		if ownerRef.UID != owner.GetUID() {
			continue
		}
		return true
	}
	return false
}

// referencedSecretNames returns the set of Secret names a ModelConfig
// Spec references via known *SecretRef fields. Used by the Update
// handler's sweep step to identify Secrets that were referenced before
// the update but aren't after — candidates for cleanup if owned by
// this ModelConfig. Add new fields here when ModelConfigSpec grows
// additional Secret-ref fields so the sweep keeps up.
func referencedSecretNames(spec v1alpha2.ModelConfigSpec) map[string]struct{} {
	refs := map[string]struct{}{}
	if spec.APIKeySecret != "" {
		refs[spec.APIKeySecret] = struct{}{}
	}
	if spec.TLS != nil && spec.TLS.CACertSecretRef != "" {
		refs[spec.TLS.CACertSecretRef] = struct{}{}
	}
	return refs
}

// deleteStaleOwnedSecret deletes a Secret in the owner's namespace if
// it carries an OwnerReference back to this owner. External Secrets
// (no matching OwnerRef) and Secrets owned by a different parent are
// left alone. NotFound is treated as success; other failures are
// logged but not returned — the caller invokes this best-effort
// after the authoritative state change has already landed.
func deleteStaleOwnedSecret(
	ctx context.Context,
	kubeClient client.Client,
	owner client.Object,
	gvk schema.GroupVersionKind,
	name string,
	log logr.Logger,
) {
	secret := &corev1.Secret{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: owner.GetNamespace(), Name: name}, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "failed to check stale companion secret", "name", name)
		}
		return
	}
	if !isOwnedBy(secret, owner, gvk) {
		return
	}
	if err := kubeClient.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "failed to delete stale companion secret", "name", name)
	}
}

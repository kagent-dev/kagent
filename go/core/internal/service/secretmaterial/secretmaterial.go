package secretmaterial

import (
	"context"
	"fmt"
	"maps"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

type Material struct {
	Name  string
	Key   string
	Value string
}

func ValidateMaterials(materials []Material) error {
	for _, material := range materials {
		if errs := validation.IsDNS1123Subdomain(material.Name); len(errs) > 0 {
			return serviceerrors.NewInvalidArgument(
				fmt.Sprintf("invalid secret name %q: %s", material.Name, strings.Join(errs, "; ")),
				nil,
			)
		}
		if errs := validation.IsConfigMapKey(material.Key); len(errs) > 0 {
			return serviceerrors.NewInvalidArgument(
				fmt.Sprintf("invalid key %q for secret %q: %s", material.Key, material.Name, strings.Join(errs, "; ")),
				nil,
			)
		}
	}
	return nil
}

func CreateCompanionSecrets(
	ctx context.Context,
	kubeClient client.Client,
	owner client.Object,
	gvk schema.GroupVersionKind,
	materials []Material,
) error {
	materialsByName := map[string]map[string][]byte{}
	for _, material := range materials {
		if _, ok := materialsByName[material.Name]; !ok {
			materialsByName[material.Name] = map[string][]byte{}
		}
		materialsByName[material.Name][material.Key] = []byte(material.Value)
	}

	namespace := owner.GetNamespace()
	for name, data := range materialsByName {
		existingSecret := &corev1.Secret{}
		err := kubeClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, existingSecret)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return serviceerrors.NewInternal(
					"Failed to create or update companion secrets",
					fmt.Errorf("failed to get companion secret %s/%s: %w", namespace, name, err),
				)
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            name,
					Namespace:       namespace,
					OwnerReferences: []metav1.OwnerReference{OwnerReferenceFor(owner, gvk)},
				},
				Type: corev1.SecretTypeOpaque,
				Data: data,
			}
			if err := kubeClient.Create(ctx, secret); err != nil {
				return serviceerrors.NewInternal(
					"Failed to create or update companion secrets",
					fmt.Errorf("failed to create companion secret %s/%s: %w", namespace, name, err),
				)
			}
			continue
		}

		if existingSecret.Type != corev1.SecretTypeOpaque {
			return serviceerrors.NewInvalidArgument(
				fmt.Sprintf(
					"companion secret %s/%s must be type %q, got %q",
					namespace,
					name,
					corev1.SecretTypeOpaque,
					existingSecret.Type,
				),
				nil,
			)
		}
		if !IsOwnedBy(existingSecret, owner, gvk) {
			return serviceerrors.NewInvalidArgument(
				fmt.Sprintf(
					"companion secret %s/%s is not managed by %s %s/%s",
					namespace,
					name,
					gvk.Kind,
					owner.GetNamespace(),
					owner.GetName(),
				),
				nil,
			)
		}

		if existingSecret.Data == nil {
			existingSecret.Data = map[string][]byte{}
		}
		maps.Copy(existingSecret.Data, data)
		if err := kubeClient.Update(ctx, existingSecret); err != nil {
			return serviceerrors.NewInternal(
				"Failed to create or update companion secrets",
				fmt.Errorf("failed to update companion secret %s/%s: %w", namespace, name, err),
			)
		}
	}

	return nil
}

func CreateOwnedOpaqueSecret(
	ctx context.Context,
	kubeClient client.Client,
	owner client.Object,
	gvk schema.GroupVersionKind,
	name string,
	data map[string]string,
) error {
	secretData := make(map[string][]byte, len(data))
	for key, value := range data {
		secretData[key] = []byte(value)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       owner.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{OwnerReferenceFor(owner, gvk)},
		},
		Type: corev1.SecretTypeOpaque,
		Data: secretData,
	}

	if err := kubeClient.Create(ctx, secret); err != nil {
		return fmt.Errorf("failed to create secret %s/%s: %w", owner.GetNamespace(), name, err)
	}
	return nil
}

func CreateOrUpdateOwnedOpaqueSecret(
	ctx context.Context,
	kubeClient client.Client,
	owner client.Object,
	gvk schema.GroupVersionKind,
	name string,
	data map[string]string,
) error {
	existingSecret := &corev1.Secret{}
	err := kubeClient.Get(ctx, client.ObjectKey{Name: name, Namespace: owner.GetNamespace()}, existingSecret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return CreateOwnedOpaqueSecret(ctx, kubeClient, owner, gvk, name, data)
		}
		return fmt.Errorf("failed to get existing secret %s/%s: %w", owner.GetNamespace(), name, err)
	}

	if existingSecret.Data == nil {
		existingSecret.Data = map[string][]byte{}
	}
	for key, value := range data {
		existingSecret.Data[key] = []byte(value)
	}
	if err := kubeClient.Update(ctx, existingSecret); err != nil {
		return fmt.Errorf("failed to update secret %s/%s: %w", owner.GetNamespace(), name, err)
	}
	return nil
}

func RollbackOwnerOnCreateFailure(ctx context.Context, kubeClient client.Client, owner client.Object) error {
	if err := kubeClient.Delete(ctx, owner); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func DeleteOwnedSecret(
	ctx context.Context,
	kubeClient client.Client,
	owner client.Object,
	gvk schema.GroupVersionKind,
	name string,
) error {
	secret := &corev1.Secret{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: owner.GetNamespace(), Name: name}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !IsOwnedBy(secret, owner, gvk) {
		return nil
	}
	if err := kubeClient.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func OwnerReferenceFor(owner client.Object, gvk schema.GroupVersionKind) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		APIVersion: gvk.GroupVersion().Identifier(),
		Kind:       gvk.Kind,
		Name:       owner.GetName(),
		UID:        owner.GetUID(),
		Controller: &controller,
	}
}

func IsOwnedBy(secret *corev1.Secret, owner client.Object, gvk schema.GroupVersionKind) bool {
	if owner.GetUID() == "" {
		return false
	}
	for _, ownerReference := range secret.GetOwnerReferences() {
		if ownerReference.APIVersion != gvk.GroupVersion().Identifier() {
			continue
		}
		if ownerReference.Kind != gvk.Kind {
			continue
		}
		if ownerReference.Name != owner.GetName() {
			continue
		}
		if ownerReference.UID != owner.GetUID() {
			continue
		}
		return true
	}
	return false
}

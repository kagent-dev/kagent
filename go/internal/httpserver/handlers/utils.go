package handlers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha2"
	common "github.com/kagent-dev/kagent/go/internal/utils"
)

// Helper function to update a reference string
func updateRef(refPtr *string, namespace string) error {
	if refPtr == nil {
		return fmt.Errorf("reference pointer cannot be nil")
	}

	ref, err := common.ParseRefString(*refPtr, namespace)
	if err != nil {
		return err
	}
	*refPtr = ref.String()
	return nil
}

// createSecretWithOwnerReference creates a Kubernetes secret with owner reference.
// Secret will have the same name and namespace as the owner object.
func createSecretWithOwnerReference(
	ctx context.Context,
	kubeClient client.Client,
	data map[string]string,
	owner client.Object,
) error {
	var ownerKind string
	switch owner.(type) {
	case *v1alpha1.Memory:
		ownerKind = "Memory"
	case *v1alpha2.ModelConfig:
		ownerKind = "ModelConfig"
	default:
		return fmt.Errorf("unsupported owner type")
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owner.GetName(),
			Namespace: owner.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: v1alpha1.GroupVersion.Identifier(),
				Kind:       ownerKind,
				Name:       owner.GetName(),
				UID:        owner.GetUID(),
				Controller: ptr.To(true),
			}},
		},
		StringData: data,
	}

	return kubeClient.Create(ctx, secret)
}

// createOrUpdateSecretWithOwnerReference creates or updates a Kubernetes secret with owner reference.
// Secret will have the same name and namespace as the owner object.
func createOrUpdateSecretWithOwnerReference(
	ctx context.Context,
	kubeClient client.Client,
	data map[string]string,
	owner client.Object,
) error {
	existingSecret := &corev1.Secret{}
	err := kubeClient.Get(ctx, client.ObjectKey{
		Name:      owner.GetName(),
		Namespace: owner.GetNamespace(),
	}, existingSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return createSecretWithOwnerReference(ctx, kubeClient, data, owner)
		}
		return fmt.Errorf("failed to get existing secret: %w", err)
	}

	existingSecret.StringData = data
	return kubeClient.Update(ctx, existingSecret)
}

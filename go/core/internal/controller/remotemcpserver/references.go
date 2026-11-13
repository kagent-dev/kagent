package remotemcpserver

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/valuesource"
)

func (r *Reconciler) updateReferenceStatus(ctx context.Context, server *v1alpha3.RemoteMCPServer) error {
	condition, resolutionError := r.referenceCondition(ctx, server)
	original := server.DeepCopy()
	if apimeta.SetStatusCondition(&server.Status.Conditions, condition) {
		if err := r.client.Status().Patch(ctx, server, client.MergeFrom(original)); err != nil {
			return fmt.Errorf("failed to update RemoteMCPServer reference status: %w", err)
		}
	}
	return resolutionError
}

func (r *Reconciler) referenceCondition(ctx context.Context, server *v1alpha3.RemoteMCPServer) (metav1.Condition, error) {
	condition := metav1.Condition{Type: v1alpha3.RemoteMCPServerConditionTypeResolvedRefs, Status: metav1.ConditionTrue, Reason: "Resolved", Message: "All header references resolved", ObservedGeneration: server.Generation}
	for index, header := range server.Spec.HeadersFrom {
		reference := header.ValueFrom
		if reference == nil {
			continue
		}
		name := types.NamespacedName{Namespace: server.Namespace, Name: reference.Name}
		_, readError := valuesource.Resolve(ctx, r.client, server.Namespace, *reference)
		if readError == nil {
			continue
		}
		var missingKeyError *valuesource.KeyNotFoundError
		missingKey := errors.As(readError, &missingKeyError)
		if !apierrors.IsNotFound(readError) && !missingKey {
			condition.Status, condition.Reason, condition.Message = metav1.ConditionUnknown, v1alpha3.RemoteMCPServerReasonReferenceResolutionFailed, fmt.Sprintf("Unable to read spec.headersFrom[%d] reference %s", index, name)
			return condition, fmt.Errorf("failed to read header reference %s: %w", name, readError)
		}
		var missingObjectReason, missingKeyReason string
		switch reference.Type {
		case v1alpha3.SecretValueSource:
			missingObjectReason, missingKeyReason = v1alpha3.RemoteMCPServerReasonSecretNotFound, v1alpha3.RemoteMCPServerReasonSecretKeyNotFound
		case v1alpha3.ConfigMapValueSource:
			missingObjectReason, missingKeyReason = v1alpha3.RemoteMCPServerReasonConfigMapNotFound, v1alpha3.RemoteMCPServerReasonConfigMapKeyNotFound
		}
		condition.Status = metav1.ConditionFalse
		condition.Reason = missingObjectReason
		if missingKey {
			condition.Reason = missingKeyReason
		}
		condition.Message = fmt.Sprintf("spec.headersFrom[%d]: reference %s key %q could not be resolved", index, name, reference.Key)
		return condition, nil
	}
	return condition, nil
}

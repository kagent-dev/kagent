package remotemcpserver

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func (r *Reconciler) updateReferenceStatus(ctx context.Context, server *v1alpha3.RemoteMCPServer) error {
	condition, resolutionError := r.referenceCondition(ctx, server)
	original := server.DeepCopy()
	apimeta.SetStatusCondition(&server.Status.Conditions, condition)
	if !reflect.DeepEqual(original.Status, server.Status) {
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
		var readError error
		var found bool
		var missingObject, missingKey string
		switch reference.Type {
		case v1alpha3.SecretValueSource:
			secret := &corev1.Secret{}
			readError = r.client.Get(ctx, name, secret)
			_, found = secret.Data[reference.Key]
			missingObject, missingKey = v1alpha3.RemoteMCPServerReasonSecretNotFound, v1alpha3.RemoteMCPServerReasonSecretKeyNotFound
		case v1alpha3.ConfigMapValueSource:
			configMap := &corev1.ConfigMap{}
			readError = r.client.Get(ctx, name, configMap)
			_, found = configMap.Data[reference.Key]
			missingObject, missingKey = v1alpha3.RemoteMCPServerReasonConfigMapNotFound, v1alpha3.RemoteMCPServerReasonConfigMapKeyNotFound
		default:
			condition.Status, condition.Reason, condition.Message = metav1.ConditionUnknown, v1alpha3.RemoteMCPServerReasonReferenceResolutionFailed, "Unsupported header reference type"
			return condition, fmt.Errorf("unsupported header reference type %q", reference.Type)
		}
		if readError != nil && !apierrors.IsNotFound(readError) {
			condition.Status, condition.Reason, condition.Message = metav1.ConditionUnknown, v1alpha3.RemoteMCPServerReasonReferenceResolutionFailed, fmt.Sprintf("Unable to read spec.headersFrom[%d] reference %s", index, name)
			return condition, fmt.Errorf("failed to read header reference %s: %w", name, readError)
		}
		if readError != nil || !found {
			condition.Status = metav1.ConditionFalse
			condition.Reason = missingKey
			if readError != nil {
				condition.Reason = missingObject
			}
			condition.Message = fmt.Sprintf("spec.headersFrom[%d]: reference %s key %q could not be resolved", index, name, reference.Key)
			return condition, nil
		}
	}
	return condition, nil
}

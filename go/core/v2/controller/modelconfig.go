package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/v2/translator"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ModelConfigReconciliation struct {
	ModelConfigName krt.Named
	// If Translation is nil, Failure is non-nil and describes why the ModelConfig could not be translated.
	// note that failure may be not nil even if Translation is non-nil.
	Translation *v2translator.ResolvedModelConfig
}

func (r ModelConfigReconciliation) Equals(other ModelConfigReconciliation) bool {
	if r.ModelConfigName != other.ModelConfigName {
		return false
	}
	if !apiequality.Semantic.DeepEqual(r.Translation, other.Translation) {
		return false
	}
	return true
}

func (r ModelConfigReconciliation) ResourceName() string {
	return r.ModelConfigName.ResourceName()
}

func newModelConfigReconciliations(
	modelConfigs krt.Collection[*kagentv1alpha3.ModelConfig],
	configMaps krt.Collection[*corev1.ConfigMap],
	secrets krt.Collection[*corev1.Secret],
	opts krt.OptionsBuilder,
) krt.StatusCollection[*kagentv1alpha3.ModelConfig, kagentv1alpha3.ModelConfigStatus] {
	statuses, _ := krt.NewStatusCollection(modelConfigs, func(ctx krt.HandlerContext, modelConfig *kagentv1alpha3.ModelConfig) (*kagentv1alpha3.ModelConfigStatus, *ModelConfigReconciliation) {
		state := &ModelConfigReconciliation{ModelConfigName: krt.Named{Namespace: modelConfig.Namespace, Name: modelConfig.Name}}
		reader := collectionReader{ctx: ctx, configMaps: configMaps, secrets: secrets, modelConfigs: modelConfigs}
		translation, err := v2translator.ResolveModelConfig(context.Background(), reader, modelConfig)
		if err != nil {
			return nil, nil
		}
		var resolvedRefsFailure *ReconciliationFailure
		var values []hashValue
		var acceptanceFailure *ReconciliationFailure
		if len(translation.SemanticFailures) > 0 {
			failure := translation.SemanticFailures[0]
			acceptanceFailure = &ReconciliationFailure{Condition: kagentv1alpha3.ModelConfigConditionTypeAccepted, Reason: failure.Reason, Message: failure.Message}
		}
		if len(translation.ReferenceFailures) > 0 {
			failure := translation.ReferenceFailures[0]
			resolvedRefsFailure = &ReconciliationFailure{Condition: kagentv1alpha3.ModelConfigConditionTypeResolvedRefs, Reason: failure.Reason, Message: failure.Message}
		}
		state.Translation = translation
		seenSecrets := map[string]struct{}{}
		for _, reference := range translation.References {
			switch reference.Kind {
			case "Secret":
				if _, seen := seenSecrets[reference.NamespacedName.String()]; seen {
					continue
				}
				secret := krt.FetchOne(ctx, secrets, krt.FilterObjectName(reference.NamespacedName))
				if secret != nil {
					values = append(values, hashValue{key: reference.NamespacedName.String(), data: (*secret).Data})
					seenSecrets[reference.NamespacedName.String()] = struct{}{}
				}
			case "ConfigMap":
				configMap := krt.FetchOne(ctx, configMaps, krt.FilterObjectName(reference.NamespacedName))
				if configMap != nil {
					values = append(values, hashValue{key: reference.NamespacedName.String(), data: map[string][]byte{reference.Key: []byte((*configMap).Data[reference.Key])}})
				}
			}
		}

		var conditions []metav1.Condition
		if acceptanceFailure != nil {
			conditions = append(conditions, metav1.Condition{
				Type:               kagentv1alpha3.ModelConfigConditionTypeAccepted,
				Status:             metav1.ConditionFalse,
				Reason:             acceptanceFailure.Reason,
				Message:            acceptanceFailure.Message,
				ObservedGeneration: modelConfig.Generation,
			})
		} else {
			conditions = append(conditions, metav1.Condition{
				Type:               kagentv1alpha3.ModelConfigConditionTypeAccepted,
				Status:             metav1.ConditionTrue,
				Reason:             "Accepted",
				Message:            "ModelConfig configuration accepted",
				ObservedGeneration: modelConfig.Generation,
			})
		}

		if resolvedRefsFailure != nil {
			conditions = append(conditions, metav1.Condition{
				Type:               kagentv1alpha3.ModelConfigConditionTypeResolvedRefs,
				Status:             metav1.ConditionFalse,
				Reason:             resolvedRefsFailure.Reason,
				Message:            resolvedRefsFailure.Message,
				ObservedGeneration: modelConfig.Generation,
			})
		} else {
			conditions = append(conditions, metav1.Condition{
				Type:               kagentv1alpha3.ModelConfigConditionTypeResolvedRefs,
				Status:             metav1.ConditionTrue,
				Reason:             "Resolved",
				Message:            "All referenced secrets and config maps resolved",
				ObservedGeneration: modelConfig.Generation,
			})
		}

		return &kagentv1alpha3.ModelConfigStatus{
			ObservedGeneration: modelConfig.Generation,
			SecretHash:         hashModelConfigValues(values),
			Conditions:         conditions,
		}, state
	}, opts.WithName("ModelConfigReconciliations")...)
	return statuses
}

type hashValue struct {
	key  string
	data map[string][]byte
}

func hashModelConfigValues(values []hashValue) string {
	slices.SortFunc(values, func(a, b hashValue) int { return strings.Compare(a.key, b.key) })
	hash := sha256.New()
	for _, value := range values {
		hash.Write([]byte(value.key))
		keys := make([]string, 0, len(value.data))
		for key := range value.data {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			hash.Write([]byte(key))
			hash.Write(value.data[key])
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func appendModelConfigFailure(current *ReconciliationFailure, condition, reason, message string) *ReconciliationFailure {
	if current != nil {
		return current
	}
	return &ReconciliationFailure{Condition: condition, Reason: reason, Message: message}
}

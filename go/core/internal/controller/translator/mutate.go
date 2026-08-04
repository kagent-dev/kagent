// Code initially copied from https://github.com/open-telemetry/opentelemetry-operator/blob/e6d96f006f05cff0bc3808da1af69b6b636fbe88/internal/manifests/mutate.go

package translator

import (
	"dario.cat/mergo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func MutateFuncFor(existing, desired client.Object) controllerutil.MutateFn {
	return func() error {
		// Get the existing annotations and override any conflicts with the desired annotations
		// This will preserve any annotations on the existing set.
		existingAnnotations := existing.GetAnnotations()
		if err := mergeWithOverride(&existingAnnotations, desired.GetAnnotations()); err != nil {
			return err
		}
		existing.SetAnnotations(existingAnnotations)

		// Get the existing labels and override any conflicts with the desired labels
		// This will preserve any labels on the existing set.
		existingLabels := existing.GetLabels()
		if err := mergeWithOverride(&existingLabels, desired.GetLabels()); err != nil {
			return err
		}
		existing.SetLabels(existingLabels)

		if ownerRefs := desired.GetOwnerReferences(); len(ownerRefs) > 0 {
			existing.SetOwnerReferences(ownerRefs)
		}

		switch existing.(type) {
		case *corev1.ConfigMap:
			cm := existing.(*corev1.ConfigMap)
			wantCm := desired.(*corev1.ConfigMap)
			mutateConfigMap(cm, wantCm)

		case *corev1.Secret:
			s := existing.(*corev1.Secret)
			wantS := desired.(*corev1.Secret)
			mutateSecret(s, wantS)

		case *corev1.Service:
			svc := existing.(*corev1.Service)
			wantSvc := desired.(*corev1.Service)
			mutateService(svc, wantSvc)

		case *corev1.ServiceAccount:
			sa := existing.(*corev1.ServiceAccount)
			wantSa := desired.(*corev1.ServiceAccount)
			mutateServiceAccount(sa, wantSa)

		case *appsv1.Deployment:
			dpl := existing.(*appsv1.Deployment)
			wantDpl := desired.(*appsv1.Deployment)
			return mutateDeployment(dpl, wantDpl)

		case *policyv1.PodDisruptionBudget:
			pdb := existing.(*policyv1.PodDisruptionBudget)
			wantPdb := desired.(*policyv1.PodDisruptionBudget)
			mutatePodDisruptionBudget(pdb, wantPdb)

		default:
			return mergeWithOverride(existing, desired)
		}
		return nil
	}
}

func mergeWithOverride(dst, src any) error {
	return mergo.Merge(dst, src, mergo.WithOverride)
}

func mutateConfigMap(existing, desired *corev1.ConfigMap) {
	existing.BinaryData = desired.BinaryData
	existing.Data = desired.Data
}

func mutateSecret(existing, desired *corev1.Secret) {
	existing.StringData = desired.StringData
	existing.Data = desired.Data
}

func mutateServiceAccount(existing, desired *corev1.ServiceAccount) {
	// Nothing to do here for the time being - we don't really care about anything but the existence of the ServiceAccount
}

func mutateService(existing, desired *corev1.Service) {
	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Selector = desired.Spec.Selector
}

// mutatePodDisruptionBudget replaces the spec wholesale rather than merging it.
//
// minAvailable and maxUnavailable are mutually exclusive, so switching an agent from
// one to the other has to clear the field it is moving away from. The default
// mergeWithOverride path cannot do that: mergo only overwrites keys present in the
// desired object and leaves the stale field populated, producing a spec the API server
// rejects for setting both.
func mutatePodDisruptionBudget(existing, desired *policyv1.PodDisruptionBudget) {
	existing.Spec = desired.Spec
}

func mutateDeployment(existing, desired *appsv1.Deployment) error {
	existing.Spec.MinReadySeconds = desired.Spec.MinReadySeconds
	existing.Spec.Paused = desired.Spec.Paused
	existing.Spec.ProgressDeadlineSeconds = desired.Spec.ProgressDeadlineSeconds
	if desired.Spec.Replicas != nil {
		// only set replicas if explicitly specified, so not to override HPA settings
		existing.Spec.Replicas = desired.Spec.Replicas
	}
	existing.Spec.RevisionHistoryLimit = desired.Spec.RevisionHistoryLimit
	existing.Spec.Strategy = desired.Spec.Strategy

	if err := mutatePodTemplate(&existing.Spec.Template, &desired.Spec.Template); err != nil {
		return err
	}

	return nil
}

func mutatePodTemplate(existing, desired *corev1.PodTemplateSpec) error {
	if err := mergeWithOverride(&existing.Labels, desired.Labels); err != nil {
		return err
	}

	if err := mergeWithOverride(&existing.Annotations, desired.Annotations); err != nil {
		return err
	}

	existing.Spec = desired.Spec

	return nil
}

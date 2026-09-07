package controller

import (
	"testing"
	"time"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestResolvedModelConfigEquals(t *testing.T) {
	left := v2translator.ResolvedModelConfig{Config: &kagentv1alpha3.ModelConfig{Spec: kagentv1alpha3.ModelConfigSpec{Model: "gpt-5"}}}
	right := v2translator.ResolvedModelConfig{Config: &kagentv1alpha3.ModelConfig{Spec: kagentv1alpha3.ModelConfigSpec{Model: "gpt-5"}}}
	if !krt.Equal(left, right) {
		t.Fatal("equal resolutions were not considered equal")
	}
	left.Config.Spec.Model = "gpt-4"
	if krt.Equal(left, right) {
		t.Fatal("different resolutions were considered equal")
	}
}

func TestModelConfigReconciliationTracksSecret(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test", nil)

	modelConfig := &kagentv1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model"},
		Spec:       kagentv1alpha3.ModelConfigSpec{Model: "gpt-5", Provider: kagentv1alpha3.ModelProviderOpenAI, APIKeySecret: "credentials"},
	}
	mock := krttest.NewMock(t, []any{modelConfig})
	modelConfigs := krttest.GetMockCollection[*kagentv1alpha3.ModelConfig](mock)
	secrets := krt.NewStaticCollection(nil, []*corev1.Secret{{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "credentials"},
		Data:       map[string][]byte{"key": []byte("before")},
	}}, opts.WithName("Secrets")...)
	configMaps := krttest.GetMockCollection[*corev1.ConfigMap](mock)
	reconciliations, _ := newModelConfigReconciliations(modelConfigs, configMaps, secrets, opts)

	waitFor(t, func() bool { return len(reconciliations.List()) == 1 })
	initial := reconciliations.List()[0].Status
	if initial.SecretHash == "" {
		t.Fatalf("unexpected initial status: %+v", initial)
	}

	secrets.UpdateObject(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "credentials"},
		Data:       map[string][]byte{"key": []byte("after")},
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && reconciliations.List()[0].Status.SecretHash == initial.SecretHash {
		time.Sleep(time.Millisecond)
	}
	if reconciliations.List()[0].Status.SecretHash == initial.SecretHash {
		t.Fatal("ModelConfig reconciliation did not change after Secret update")
	}
}

func TestModelConfigReconciliationMissingAPIKeySecretKey(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test", nil)

	modelConfig := &kagentv1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model"},
		Spec: kagentv1alpha3.ModelConfigSpec{
			Model:           "gpt-5",
			Provider:        kagentv1alpha3.ModelProviderOpenAI,
			APIKeySecret:    "credentials",
			APIKeySecretKey: "NON_EXISTENT_KEY",
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "credentials"},
		Data:       map[string][]byte{"EXISTING_KEY": []byte("secret-value")},
	}
	mock := krttest.NewMock(t, []any{modelConfig, secret})
	modelConfigs := krttest.GetMockCollection[*kagentv1alpha3.ModelConfig](mock)
	secrets := krttest.GetMockCollection[*corev1.Secret](mock)
	configMaps := krttest.GetMockCollection[*corev1.ConfigMap](mock)
	reconciliations, _ := newModelConfigReconciliations(modelConfigs, configMaps, secrets, opts)

	waitFor(t, func() bool { return len(reconciliations.List()) == 1 })
	status := reconciliations.List()[0].Status
	acceptedCond := apimeta.FindStatusCondition(status.Conditions, kagentv1alpha3.ModelConfigConditionTypeAccepted)
	if acceptedCond == nil || acceptedCond.Status != metav1.ConditionTrue {
		t.Fatalf("expected Accepted condition with Status=True, got: %+v", acceptedCond)
	}
	resolvedRefsCond := apimeta.FindStatusCondition(status.Conditions, kagentv1alpha3.ModelConfigConditionTypeResolvedRefs)
	if resolvedRefsCond == nil || resolvedRefsCond.Status != metav1.ConditionFalse || resolvedRefsCond.Reason != "APIKeySecretKeyNotFound" {
		t.Fatalf("expected ResolvedRefs condition with Status=False and Reason=APIKeySecretKeyNotFound, got: %+v", resolvedRefsCond)
	}
}

func TestModelConfigReconciliationValidatesEffectiveProviderReferences(t *testing.T) {
	tests := []struct {
		name           string
		spec           kagentv1alpha3.ModelConfigSpec
		secret         *corev1.Secret
		configMap      *corev1.ConfigMap
		expectedReason string
	}{
		{
			name: "TLS CA key", spec: kagentv1alpha3.ModelConfigSpec{
				Model: "gpt-5", Provider: kagentv1alpha3.ModelProviderOpenAI,
				TLS: &kagentv1alpha3.TLSConfig{CACertSecretRef: "ca", CACertSecretKey: "ca.pem"},
			}, secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "ca"}}, expectedReason: "TLSSecretKeyNotFound",
		},
		{
			name: "SAP credentials", spec: kagentv1alpha3.ModelConfigSpec{
				Model: "gpt-5", Provider: kagentv1alpha3.ModelProviderSAPAICore,
				SAPAICore: &kagentv1alpha3.SAPAICoreConfig{BaseURL: "https://sap.example.com"}, APIKeySecret: "credentials",
			}, secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "credentials"}, Data: map[string][]byte{"client_id": []byte("id")}}, expectedReason: "SAPAICoreCredentialKeyNotFound",
		},
		{
			name: "Bedrock IAM credentials", spec: kagentv1alpha3.ModelConfigSpec{
				Model: "claude", Provider: kagentv1alpha3.ModelProviderBedrock,
				Bedrock: &kagentv1alpha3.BedrockConfig{Region: "us-east-1"}, APIKeySecret: "credentials",
			}, secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "credentials"}, Data: map[string][]byte{"AWS_ACCESS_KEY_ID": []byte("access")}}, expectedReason: "BedrockCredentialKeyNotFound",
		},
		{
			name: "Foundry endpoint", spec: kagentv1alpha3.ModelConfigSpec{
				Model: "gpt-5", Provider: kagentv1alpha3.ModelProviderFoundry,
				Foundry: &kagentv1alpha3.FoundryConfig{Deployment: "chat", EndpointFrom: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "account"}, Key: "endpoint"}},
			}, configMap: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "account"}}, expectedReason: "EndpointConfigMapKeyNotFound",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stop := make(chan struct{})
			t.Cleanup(func() { close(stop) })
			opts := krt.NewOptionsBuilder(stop, "test", nil)
			modelConfig := &kagentv1alpha3.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model"}, Spec: test.spec,
			}
			inputs := []any{modelConfig}
			if test.secret != nil {
				inputs = append(inputs, test.secret)
			}
			if test.configMap != nil {
				inputs = append(inputs, test.configMap)
			}
			mock := krttest.NewMock(t, inputs)
			modelConfigs := krttest.GetMockCollection[*kagentv1alpha3.ModelConfig](mock)
			secretCollection := krttest.GetMockCollection[*corev1.Secret](mock)
			configMapCollection := krttest.GetMockCollection[*corev1.ConfigMap](mock)
			reconciliations, _ := newModelConfigReconciliations(modelConfigs, configMapCollection, secretCollection, opts)

			waitFor(t, func() bool { return len(reconciliations.List()) == 1 })
			status := reconciliations.List()[0].Status
			accepted := apimeta.FindStatusCondition(status.Conditions, kagentv1alpha3.ModelConfigConditionTypeAccepted)
			if accepted == nil || accepted.Status != metav1.ConditionTrue {
				t.Fatalf("expected Accepted=True, got: %+v", accepted)
			}
			resolvedRefs := apimeta.FindStatusCondition(status.Conditions, kagentv1alpha3.ModelConfigConditionTypeResolvedRefs)
			if resolvedRefs == nil || resolvedRefs.Status != metav1.ConditionFalse || resolvedRefs.Reason != test.expectedReason {
				t.Fatalf("expected ResolvedRefs=False with Reason=%q, got: %+v", test.expectedReason, resolvedRefs)
			}
		})
	}
}

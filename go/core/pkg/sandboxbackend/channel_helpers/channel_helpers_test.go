package channel_helpers

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCredentialContainerEnvInline(t *testing.T) {
	env, err := CredentialContainerEnv(v1alpha2.AgentHarnessChannelCredential{Value: "  xoxb-123  "}, "SLACK_BOT_TOKEN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Name != "SLACK_BOT_TOKEN" || env.Value != "xoxb-123" || env.ValueFrom != nil {
		t.Fatalf("unexpected env: %+v", env)
	}
}

func TestCredentialContainerEnvSecret(t *testing.T) {
	env, err := CredentialContainerEnv(v1alpha2.AgentHarnessChannelCredential{
		ValueFrom: &v1alpha2.ValueSource{Type: v1alpha2.SecretValueSource, Name: "sec", Key: "tok"},
	}, "TELEGRAM_BOT_TOKEN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Value != "" || env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("expected secret valueFrom, got %+v", env)
	}
	if env.ValueFrom.SecretKeyRef.Name != "sec" || env.ValueFrom.SecretKeyRef.Key != "tok" {
		t.Fatalf("unexpected secret ref: %+v", env.ValueFrom.SecretKeyRef)
	}
}

func TestCredentialContainerEnvConfigMap(t *testing.T) {
	env, err := CredentialContainerEnv(v1alpha2.AgentHarnessChannelCredential{
		ValueFrom: &v1alpha2.ValueSource{Type: v1alpha2.ConfigMapValueSource, Name: "cm", Key: "tok"},
	}, "SLACK_APP_TOKEN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.ValueFrom == nil || env.ValueFrom.ConfigMapKeyRef == nil {
		t.Fatalf("expected configmap valueFrom, got %+v", env)
	}
	if env.ValueFrom.ConfigMapKeyRef.Name != "cm" || env.ValueFrom.ConfigMapKeyRef.Key != "tok" {
		t.Fatalf("unexpected configmap ref: %+v", env.ValueFrom.ConfigMapKeyRef)
	}
}

func TestCredentialContainerEnvErrors(t *testing.T) {
	if _, err := CredentialContainerEnv(v1alpha2.AgentHarnessChannelCredential{}, "X"); err == nil {
		t.Fatal("expected error when neither value nor valueFrom is set")
	}
	if _, err := CredentialContainerEnv(v1alpha2.AgentHarnessChannelCredential{
		ValueFrom: &v1alpha2.ValueSource{Type: "Bogus", Name: "n", Key: "k"},
	}, "X"); err == nil {
		t.Fatal("expected error for unknown value source type")
	}
}

func TestSplitList(t *testing.T) {
	got := SplitList(" a , b\n c ; ;d ")
	want := []string{"a", "b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if SplitList("   ") != nil {
		t.Fatal("expected nil for blank input")
	}
}

func TestTrimNonEmpty(t *testing.T) {
	got := TrimNonEmpty([]string{" a ", "", "  ", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestResolveAllowedUserIDsInline(t *testing.T) {
	got, err := ResolveAllowedUserIDs(context.Background(), nil, "ns", []string{" u1 ", "", "u2"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "u1" || got[1] != "u2" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestResolveAllowedUserIDsFromSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "allow", Namespace: "ns"},
		Data:       map[string][]byte{"users": []byte("u1,u2\nu3")},
	}).Build()

	got, err := ResolveAllowedUserIDs(context.Background(), kube, "ns", nil, &v1alpha2.ValueSource{
		Type: v1alpha2.SecretValueSource, Name: "allow", Key: "users",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 || got[0] != "u1" || got[2] != "u3" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestResolveAllowedUserIDsNone(t *testing.T) {
	got, err := ResolveAllowedUserIDs(context.Background(), nil, "ns", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

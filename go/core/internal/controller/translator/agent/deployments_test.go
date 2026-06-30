package agent

import (
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestResolveByoDeployment_NilReplicasPreserved(t *testing.T) {
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_BYO,
			BYO: &v1alpha2.BYOAgentSpec{
				Deployment: &v1alpha2.ByoDeploymentSpec{
					Image: "my-image:latest",
				},
			},
		},
	}
	dep, err := resolveByoDeployment(agent)
	if err != nil {
		t.Fatalf("resolveByoDeployment() error = %v", err)
	}
	if dep.Replicas != nil {
		t.Errorf("Replicas = %v, want nil so HPA can manage replicas", *dep.Replicas)
	}
}

func TestValidateExtraContainers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		containers []corev1.Container
		wantErr    bool
	}{
		{
			name:       "empty list is fine",
			containers: nil,
			wantErr:    false,
		},
		{
			name: "normal sidecar names are fine",
			containers: []corev1.Container{
				{Name: "envoy"},
				{Name: "log-shipper"},
			},
			wantErr: false,
		},
		{
			name: "reserved name kagent is rejected",
			containers: []corev1.Container{
				{Name: "kagent"},
			},
			wantErr: true,
		},
		{
			name: "duplicate sidecar names are rejected",
			containers: []corev1.Container{
				{Name: "proxy"},
				{Name: "proxy"},
			},
			wantErr: true,
		},
		{
			name: "kagent mixed with other containers is still rejected",
			containers: []corev1.Container{
				{Name: "envoy"},
				{Name: "kagent"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateExtraContainers(tt.containers)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateExtraContainers() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMergeRuntimeRequirements(t *testing.T) {
	t.Parallel()

	t.Run("nil inputs are ignored", func(t *testing.T) {
		t.Parallel()
		if err := mergeRuntimeRequirements(nil, &modelRuntimeRequirements{PodLabels: map[string]string{"a": "b"}}); err != nil {
			t.Fatalf("mergeRuntimeRequirements(nil, src) error = %v", err)
		}
		dst := &modelRuntimeRequirements{}
		if err := mergeRuntimeRequirements(dst, nil); err != nil {
			t.Fatalf("mergeRuntimeRequirements(dst, nil) error = %v", err)
		}
	})

	t.Run("merges labels and annotations", func(t *testing.T) {
		t.Parallel()
		dst := &modelRuntimeRequirements{
			PodLabels:                 map[string]string{"existing-label": "same"},
			ServiceAccountAnnotations: map[string]string{"existing-annotation": "same"},
		}
		src := &modelRuntimeRequirements{
			PodLabels: map[string]string{
				"existing-label": "same",
				"new-label":      "value",
			},
			ServiceAccountAnnotations: map[string]string{
				"existing-annotation": "same",
				"new-annotation":      "value",
			},
		}

		if err := mergeRuntimeRequirements(dst, src); err != nil {
			t.Fatalf("mergeRuntimeRequirements() error = %v", err)
		}
		if got := dst.PodLabels["new-label"]; got != "value" {
			t.Fatalf("new label = %q, want value", got)
		}
		if got := dst.ServiceAccountAnnotations["new-annotation"]; got != "value" {
			t.Fatalf("new annotation = %q, want value", got)
		}
	})

	t.Run("rejects conflicting pod labels", func(t *testing.T) {
		t.Parallel()
		dst := &modelRuntimeRequirements{PodLabels: map[string]string{"identity/use": "true"}}
		src := &modelRuntimeRequirements{PodLabels: map[string]string{"identity/use": "false"}}

		err := mergeRuntimeRequirements(dst, src)
		if err == nil || !strings.Contains(err.Error(), `conflicting pod label "identity/use"`) {
			t.Fatalf("mergeRuntimeRequirements() error = %v, want conflicting pod label", err)
		}
	})

	t.Run("rejects conflicting service account annotations", func(t *testing.T) {
		t.Parallel()
		dst := &modelRuntimeRequirements{ServiceAccountAnnotations: map[string]string{"identity/client-id": "one"}}
		src := &modelRuntimeRequirements{ServiceAccountAnnotations: map[string]string{"identity/client-id": "two"}}

		err := mergeRuntimeRequirements(dst, src)
		if err == nil || !strings.Contains(err.Error(), `conflicting service account annotation "identity/client-id"`) {
			t.Fatalf("mergeRuntimeRequirements() error = %v, want conflicting service account annotation", err)
		}
	})
}

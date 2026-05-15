package agent

import (
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

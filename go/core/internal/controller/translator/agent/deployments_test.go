package agent

import (
	"reflect"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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

func TestGetDefaultNodeSelector(t *testing.T) {
	setGlobal := func(t *testing.T, v map[string]string) {
		t.Helper()
		prev := DefaultAgentNodeSelector
		DefaultAgentNodeSelector = v
		t.Cleanup(func() { DefaultAgentNodeSelector = prev })
	}

	t.Run("nil stays nil without a global default", func(t *testing.T) {
		setGlobal(t, nil)
		if got := getDefaultNodeSelector(nil); got != nil {
			t.Errorf("getDefaultNodeSelector(nil) = %v, want nil", got)
		}
	})

	t.Run("per-agent value kept without a global default", func(t *testing.T) {
		setGlobal(t, nil)
		got := getDefaultNodeSelector(map[string]string{"disktype": "ssd"})
		if got["disktype"] != "ssd" || len(got) != 1 {
			t.Errorf("getDefaultNodeSelector() = %v, want map[disktype:ssd]", got)
		}
	})

	t.Run("global default applied when agent sets nothing", func(t *testing.T) {
		setGlobal(t, map[string]string{"kubernetes.io/os": "linux"})
		got := getDefaultNodeSelector(nil)
		if got["kubernetes.io/os"] != "linux" || len(got) != 1 {
			t.Errorf("getDefaultNodeSelector(nil) = %v, want the global default", got)
		}
	})

	t.Run("per-agent nodeSelector overrides the global default", func(t *testing.T) {
		setGlobal(t, map[string]string{"kubernetes.io/os": "linux", "pool": "default"})
		got := getDefaultNodeSelector(map[string]string{"pool": "gpu"})
		if got["pool"] != "gpu" || got["kubernetes.io/os"] != "linux" || len(got) != 2 {
			t.Errorf("getDefaultNodeSelector() = %v, want merged map with per-agent pool=gpu", got)
		}
	})
}

func TestGetDefaultDeploymentStrategy(t *testing.T) {
	intOrStr := func(v int32) *intstr.IntOrString {
		return &intstr.IntOrString{Type: intstr.Int, IntVal: v}
	}
	defaultStrategy := appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{
			MaxUnavailable: intOrStr(0),
			MaxSurge:       intOrStr(1),
		},
	}
	fromString := intstr.FromString("50%")

	tests := []struct {
		name  string
		input *appsv1.DeploymentStrategy
		want  appsv1.DeploymentStrategy
	}{
		{
			name:  "nil falls back to default RollingUpdate",
			input: nil,
			want:  defaultStrategy,
		},
		{
			name:  "Recreate passes through unchanged",
			input: &appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			want:  appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
		},
		{
			name:  "empty type defaults to RollingUpdate with defaults filled",
			input: &appsv1.DeploymentStrategy{},
			want:  defaultStrategy,
		},
		{
			name:  "RollingUpdate with nil rollingUpdate block gets defaults filled",
			input: &appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType},
			want:  defaultStrategy,
		},
		{
			name: "partial rollingUpdate keeps user value and fills the rest",
			input: &appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge: &fromString,
				},
			},
			want: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: intOrStr(0),
					MaxSurge:       &fromString,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getDefaultDeploymentStrategy(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getDefaultDeploymentStrategy() = %+v, want %+v", got, tt.want)
			}
			if tt.input != nil && tt.input.RollingUpdate != nil && got.RollingUpdate == tt.input.RollingUpdate {
				t.Error("getDefaultDeploymentStrategy() must not alias the input's RollingUpdate pointer")
			}
		})
	}
}

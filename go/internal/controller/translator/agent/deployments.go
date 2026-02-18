package agent

import (
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/controller/translator/labels"
)

// Internal to translator - Data added to the deployment spec for an inline agent
// Mostly used for model auth and config.
type modelDeploymentData struct {
	EnvVars      []corev1.EnvVar
	Volumes      []corev1.Volume
	VolumeMounts []corev1.VolumeMount
}

// Internal to translator – a unified deployment spec for any agent.
type resolvedDeployment struct {
	// Required concrete runtime properties
	Image           string
	Cmd             string // empty → no explicit command
	Args            []string
	Port            int32 // container port and Service port
	ImagePullPolicy corev1.PullPolicy

	// SharedDeploymentSpec merged
	Replicas             *int32
	ImagePullSecrets     []corev1.LocalObjectReference
	Volumes              []corev1.Volume
	VolumeMounts         []corev1.VolumeMount
	Labels               map[string]string
	Annotations          map[string]string
	Env                  []corev1.EnvVar
	Resources            corev1.ResourceRequirements
	Tolerations          []corev1.Toleration
	Affinity             *corev1.Affinity
	NodeSelector         map[string]string
	SecurityContext      *corev1.SecurityContext
	PodSecurityContext   *corev1.PodSecurityContext
	ServiceAccountName   *string
	ServiceAccountConfig *v1alpha2.ServiceAccountConfig
}

// getDefaultResources sets default resource requirements if not specified
func getDefaultResources(spec *corev1.ResourceRequirements) corev1.ResourceRequirements {
	if spec == nil {
		return corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("384Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		}
	}
	return *spec
}

func getDefaultLabels(agentName string, incoming map[string]string) map[string]string {
	defaultLabels := map[string]string{
		labels.AppManagedBy: labels.ManagedByKagent,
		labels.AppPartOf:    labels.ManagedByKagent,
		labels.AppName:      agentName,
	}
	maps.Copy(defaultLabels, incoming)
	return defaultLabels
}

func resolveInlineDeployment(agent *v1alpha2.Agent, mdd *modelDeploymentData) (*resolvedDeployment, error) {
	// Defaults
	port := int32(8080)
	args := []string{
		"--host",
		"0.0.0.0",
		"--port",
		fmt.Sprintf("%d", port),
		"--filepath",
		"/config",
	}

	serviceAccountName := ptr.To(agent.Name)

	// Start with spec deployment spec
	spec := v1alpha2.DeclarativeDeploymentSpec{}
	if agent.Spec.Declarative.Deployment != nil {
		spec = *agent.Spec.Declarative.Deployment
	}
	registry := DefaultImageConfig.Registry
	if spec.ImageRegistry != "" {
		registry = spec.ImageRegistry
	}
	repository := DefaultImageConfig.Repository
	image := fmt.Sprintf("%s/%s:%s", registry, repository, DefaultImageConfig.Tag)

	imagePullPolicy := corev1.PullPolicy(DefaultImageConfig.PullPolicy)
	if spec.ImagePullPolicy != "" {
		imagePullPolicy = corev1.PullPolicy(spec.ImagePullPolicy)
	}

	if DefaultImageConfig.PullSecret != "" {
		// Only append if not already present
		alreadyPresent := checkPullSecretAlreadyPresent(spec)
		if !alreadyPresent {
			spec.ImagePullSecrets = append(spec.ImagePullSecrets, corev1.LocalObjectReference{Name: DefaultImageConfig.PullSecret})
		}
	}

	dep := &resolvedDeployment{
		Image:                image,
		Args:                 args,
		Port:                 port,
		ImagePullPolicy:      imagePullPolicy,
		Replicas:             spec.Replicas,
		ImagePullSecrets:     slices.Clone(spec.ImagePullSecrets),
		Volumes:              append(slices.Clone(spec.Volumes), mdd.Volumes...),
		VolumeMounts:         append(slices.Clone(spec.VolumeMounts), mdd.VolumeMounts...),
		Labels:               getDefaultLabels(agent.Name, spec.Labels),
		Annotations:          maps.Clone(spec.Annotations),
		Env:                  append(slices.Clone(spec.Env), mdd.EnvVars...),
		Resources:            getDefaultResources(spec.Resources), // Set default resources if not specified
		Tolerations:          slices.Clone(spec.Tolerations),
		Affinity:             spec.Affinity,
		NodeSelector:         maps.Clone(spec.NodeSelector),
		SecurityContext:      spec.SecurityContext,
		PodSecurityContext:   spec.PodSecurityContext,
		ServiceAccountName:   spec.ServiceAccountName,
		ServiceAccountConfig: spec.ServiceAccountConfig,
	}

	// If not specified, use the agent name as the service account name
	if dep.ServiceAccountName == nil {
		dep.ServiceAccountName = serviceAccountName
	}

	return dep, nil
}

func checkPullSecretAlreadyPresent(spec v1alpha2.DeclarativeDeploymentSpec) bool {
	alreadyPresent := false
	for _, secret := range spec.ImagePullSecrets {
		if secret.Name == DefaultImageConfig.PullSecret {
			alreadyPresent = true
			break
		}
	}
	return alreadyPresent
}

func resolveByoDeployment(agent *v1alpha2.Agent) (*resolvedDeployment, error) {
	spec := agent.Spec.BYO.Deployment
	if spec == nil {
		return nil, fmt.Errorf("BYO deployment spec is required")
	}

	// Defaults
	port := int32(8080)

	image := spec.Image
	if image == "" {
		// This should never happen as it's required by the API
		return nil, fmt.Errorf("image is required for BYO deployment")
	}

	cmd := ""
	if spec.Cmd != nil && *spec.Cmd != "" {
		cmd = *spec.Cmd
	}

	var args []string
	if len(spec.Args) != 0 {
		args = spec.Args
	}

	imagePullPolicy := corev1.PullPolicy(DefaultImageConfig.PullPolicy)
	if spec.ImagePullPolicy != "" {
		imagePullPolicy = corev1.PullPolicy(spec.ImagePullPolicy)
	}

	replicas := spec.Replicas
	if replicas == nil {
		replicas = ptr.To(int32(1))
	}

	dep := &resolvedDeployment{
		Image:                image,
		Cmd:                  cmd,
		Args:                 args,
		Port:                 port,
		ImagePullPolicy:      imagePullPolicy,
		Replicas:             replicas,
		ImagePullSecrets:     slices.Clone(spec.ImagePullSecrets),
		Volumes:              slices.Clone(spec.Volumes),
		VolumeMounts:         slices.Clone(spec.VolumeMounts),
		Labels:               getDefaultLabels(agent.Name, spec.Labels),
		Annotations:          maps.Clone(spec.Annotations),
		Env:                  slices.Clone(spec.Env),
		Resources:            getDefaultResources(spec.Resources), // Set default resources if not specified
		Tolerations:          slices.Clone(spec.Tolerations),
		Affinity:             spec.Affinity,
		NodeSelector:         maps.Clone(spec.NodeSelector),
		SecurityContext:      spec.SecurityContext,
		PodSecurityContext:   spec.PodSecurityContext,
		ServiceAccountName:   spec.ServiceAccountName,
		ServiceAccountConfig: spec.ServiceAccountConfig,
	}

	if dep.ServiceAccountName == nil {
		dep.ServiceAccountName = ptr.To(agent.Name)
	}

	return dep, nil
}

package agent

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/controller/translator/labels"
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
	ExtraContainers      []corev1.Container
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
	// Global default labels (from --default-agent-pod-labels flag) override built-in defaults
	maps.Copy(defaultLabels, DefaultAgentPodLabels)
	// Per-agent labels override global defaults
	maps.Copy(defaultLabels, incoming)
	return defaultLabels
}

// getRuntimeImageRepository returns the image repository for a given runtime.
// It respects DefaultImageConfig.Repository for the Python runtime, and derives
// the Go runtime repository by replacing the last path segment with "golang-adk".
// This ensures custom repository configurations (e.g., --image-repository flag) work correctly.
func getRuntimeImageRepository(runtime v1alpha2.DeclarativeRuntime) string {
	switch runtime {
	case v1alpha2.DeclarativeRuntime_Go:
		// Derive Go runtime repository from the default Python repository
		// by replacing the last segment (typically "app") with "golang-adk".
		// This respects any custom repository configuration.
		pythonRepo := DefaultImageConfig.Repository
		lastSlash := strings.LastIndex(pythonRepo, "/")
		if lastSlash == -1 {
			// No slash found, repository is just the image name
			return "golang-adk"
		}
		baseRepo := pythonRepo[:lastSlash]
		return baseRepo + "/golang-adk"
	case v1alpha2.DeclarativeRuntime_Python:
		// Use the configured Python repository as-is
		return DefaultImageConfig.Repository
	default:
		// Default to Python (should never happen due to enum validation)
		return DefaultImageConfig.Repository
	}
}

// validateExtraContainers checks that none of the extra containers use the
// reserved name "kagent" and that no two containers share the same name.
func validateExtraContainers(containers []corev1.Container) error {
	seen := make(map[string]bool)
	for _, c := range containers {
		if c.Name == "kagent" {
			return fmt.Errorf("extraContainers: %q is a reserved container name", c.Name)
		}
		if seen[c.Name] {
			return fmt.Errorf("extraContainers: duplicate container name %q", c.Name)
		}
		seen[c.Name] = true
	}
	return nil
}

func resolveInlineDeployment(agent v1alpha2.AgentObject, mdd *modelDeploymentData) (*resolvedDeployment, error) {
	specRef := agent.GetAgentSpec()
	// Defaults
	port := int32(8080)
	args := []string{
		"--host",
		DefaultAgentBindHost,
		"--port",
		fmt.Sprintf("%d", port),
		"--filepath",
		"/config",
	}

	serviceAccountName := new(string)
	*serviceAccountName = agent.GetName()

	// Start with spec deployment spec
	spec := v1alpha2.DeclarativeDeploymentSpec{}
	if specRef.Declarative.Deployment != nil {
		spec = *specRef.Declarative.Deployment
	}

	// Determine runtime (defaults to python if not set)
	runtime := v1alpha2.DeclarativeRuntime_Python
	if specRef.Declarative.Runtime != "" {
		runtime = specRef.Declarative.Runtime
	}

	// Get registry
	registry := DefaultImageConfig.Registry
	if spec.ImageRegistry != "" {
		registry = spec.ImageRegistry
	}

	repository := getRuntimeImageRepository(runtime)

	tag := DefaultImageConfig.Tag
	if runtime == v1alpha2.DeclarativeRuntime_Go && needsSRTSettings(agent, specRef.Sandbox) {
		tag += "-full"
	}

	image := fmt.Sprintf("%s/%s:%s", registry, repository, tag)

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

	if err := validateExtraContainers(spec.ExtraContainers); err != nil {
		return nil, err
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
		Labels:               getDefaultLabels(agent.GetName(), spec.Labels),
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
		ExtraContainers:      slices.Clone(spec.ExtraContainers),
	}

	// Precedence: agent-level serviceAccountName > global default > auto-created SA (agent name)
	if dep.ServiceAccountName == nil {
		if DefaultServiceAccountName != "" {
			dep.ServiceAccountName = new(DefaultServiceAccountName)
		} else {
			dep.ServiceAccountName = serviceAccountName
		}
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

func resolveByoDeployment(agent v1alpha2.AgentObject) (*resolvedDeployment, error) {
	spec := agent.GetAgentSpec().BYO.Deployment
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

	if err := validateExtraContainers(spec.ExtraContainers); err != nil {
		return nil, err
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
		Labels:               getDefaultLabels(agent.GetName(), spec.Labels),
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
		ExtraContainers:      slices.Clone(spec.ExtraContainers),
	}

	// Precedence: agent-level serviceAccountName > global default > auto-created SA (agent name)
	if dep.ServiceAccountName == nil {
		if DefaultServiceAccountName != "" {
			dep.ServiceAccountName = new(string)
			*dep.ServiceAccountName = DefaultServiceAccountName
		} else {
			dep.ServiceAccountName = new(string)
			*dep.ServiceAccountName = agent.GetName()
		}
	}

	return dep, nil
}

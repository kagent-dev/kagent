package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/controller/translator/labels"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

type manifestContext struct {
	agent          v1alpha2.AgentObject
	deployment     *resolvedDeployment
	selectorLabels map[string]string
}

type configSecretInputs struct {
	secret     *corev1.Secret
	configHash uint64
	volumes    []corev1.Volume
	mounts     []corev1.VolumeMount
}

type podRuntimeInputs struct {
	initContainers  []corev1.Container
	envVars         []corev1.EnvVar
	volumes         []corev1.Volume
	volumeMounts    []corev1.VolumeMount
	securityContext *corev1.SecurityContext
}

func (a *adkApiTranslator) BuildManifest(
	ctx context.Context,
	agent v1alpha2.AgentObject,
	inputs *AgentManifestInputs,
) (*AgentOutputs, error) {
	if inputs == nil {
		return nil, fmt.Errorf("agent manifest inputs are required")
	}
	if inputs.Deployment == nil {
		return nil, fmt.Errorf("resolved deployment is required")
	}

	outputs := &AgentOutputs{}
	manifestCtx := newManifestContext(agent, inputs.Deployment)

	configSecret, err := a.buildConfigSecret(manifestCtx, inputs.Config, inputs.Sandbox, inputs.AgentCard, inputs.SecretHashBytes)
	if err != nil {
		return nil, err
	}
	outputs.Manifest = append(outputs.Manifest, configSecret.secret)

	if sa := buildServiceAccount(manifestCtx); sa != nil {
		outputs.Manifest = append(outputs.Manifest, sa)
	}

	podRuntime, err := buildPodRuntime(manifestCtx, inputs.Config, inputs.Sandbox, configSecret.volumes, configSecret.mounts)
	if err != nil {
		return nil, err
	}

	podTemplate := buildPodTemplate(manifestCtx, podRuntime, configSecret.configHash)

	workloadObjects, err := a.buildWorkloadObjects(ctx, manifestCtx, podTemplate)
	if err != nil {
		return nil, err
	}
	outputs.Manifest = append(outputs.Manifest, workloadObjects...)

	if err := a.setManifestOwnerReferences(agent, outputs.Manifest); err != nil {
		return nil, err
	}

	outputs.Config = inputs.Config
	if inputs.AgentCard != nil {
		outputs.AgentCard = *inputs.AgentCard
	}

	return outputs, a.runPlugins(ctx, agent, outputs)
}

func newManifestContext(agent v1alpha2.AgentObject, dep *resolvedDeployment) manifestContext {
	return manifestContext{
		agent:      agent,
		deployment: dep,
		selectorLabels: map[string]string{
			"app":    labels.ManagedByKagent,
			"kagent": agent.GetName(),
		},
	}
}

func (m manifestContext) runInSandbox() bool {
	return m.agent.GetWorkloadMode() == v1alpha2.WorkloadModeSandbox
}

func (m manifestContext) podLabels() map[string]string {
	podLabels := maps.Clone(m.selectorLabels)
	if m.deployment.Labels != nil {
		maps.Copy(podLabels, m.deployment.Labels)
	}
	return podLabels
}

func (m manifestContext) objectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:        m.agent.GetName(),
		Namespace:   m.agent.GetNamespace(),
		Annotations: m.agent.GetAnnotations(),
		Labels:      m.podLabels(),
	}
}

func (a *adkApiTranslator) buildConfigSecret(
	manifestCtx manifestContext,
	cfg *adk.AgentConfig,
	sandboxCfg *v1alpha2.SandboxConfig,
	card *server.AgentCard,
	modelConfigSecretHashBytes []byte,
) (*configSecretInputs, error) {
	cfgJSON := ""
	agentCard := ""
	srtSettingsJSON := ""
	var configHash uint64
	var volumes []corev1.Volume
	var mounts []corev1.VolumeMount

	if cfg != nil {
		bCfg, err := json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
		cfgJSON = string(bCfg)
	}
	if card != nil {
		bCard, err := json.Marshal(card)
		if err != nil {
			return nil, err
		}
		agentCard = string(bCard)
	}
	if needsSRTSettings(manifestCtx.agent, sandboxCfg) {
		bSRTSettings, err := buildSRTSettingsJSON(sandboxCfg)
		if err != nil {
			return nil, err
		}
		srtSettingsJSON = string(bSRTSettings)
	}

	if cfg != nil || srtSettingsJSON != "" {
		secretData := modelConfigSecretHashBytes
		if secretData == nil {
			secretData = []byte{}
		}
		hashData := make([]byte, 0, len(secretData)+len(srtSettingsJSON))
		hashData = append(hashData, secretData...)
		hashData = append(hashData, srtSettingsJSON...)
		configHash = computeConfigHash([]byte(cfgJSON), []byte(agentCard), hashData)
		volumes = []corev1.Volume{{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: manifestCtx.agent.GetName()},
			},
		}}
		mounts = []corev1.VolumeMount{{Name: "config", MountPath: "/config"}}
	}

	return &configSecretInputs{
		secret: &corev1.Secret{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: manifestCtx.objectMeta(),
			StringData: buildConfigSecretData(cfgJSON, agentCard, srtSettingsJSON),
		},
		configHash: configHash,
		volumes:    volumes,
		mounts:     mounts,
	}, nil
}

func buildConfigSecretData(cfgJSON, agentCard, srtSettingsJSON string) map[string]string {
	data := map[string]string{
		"config.json":     cfgJSON,
		"agent-card.json": agentCard,
	}
	if srtSettingsJSON != "" {
		data["srt-settings.json"] = srtSettingsJSON
	}
	return data
}

func buildServiceAccount(manifestCtx manifestContext) *corev1.ServiceAccount {
	serviceAccountName := manifestCtx.deployment.ServiceAccountName
	if serviceAccountName == nil || *serviceAccountName != manifestCtx.agent.GetName() {
		return nil
	}

	sa := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: manifestCtx.objectMeta(),
	}

	if manifestCtx.deployment.ServiceAccountConfig == nil {
		return sa
	}

	if manifestCtx.deployment.ServiceAccountConfig.Labels != nil {
		if sa.Labels == nil {
			sa.Labels = make(map[string]string)
		}
		maps.Copy(sa.Labels, manifestCtx.deployment.ServiceAccountConfig.Labels)
	}
	if manifestCtx.deployment.ServiceAccountConfig.Annotations != nil {
		if sa.Annotations == nil {
			sa.Annotations = make(map[string]string)
		}
		maps.Copy(sa.Annotations, manifestCtx.deployment.ServiceAccountConfig.Annotations)
	}

	return sa
}

func buildPodRuntime(
	manifestCtx manifestContext,
	cfg *adk.AgentConfig,
	sandboxCfg *v1alpha2.SandboxConfig,
	secretVolumes []corev1.Volume,
	secretMounts []corev1.VolumeMount,
) (*podRuntimeInputs, error) {
	sharedEnv := collectSharedEnv(manifestCtx.agent)

	volumes := append([]corev1.Volume{}, secretVolumes...)
	volumes = append(volumes, manifestCtx.deployment.Volumes...)
	volumeMounts := append([]corev1.VolumeMount{}, secretMounts...)
	volumeMounts = append(volumeMounts, manifestCtx.deployment.VolumeMounts...)

	needCodeExecIsolation := cfg != nil && cfg.GetExecuteCode()
	initContainers, err := buildSkillsRuntime(manifestCtx, &sharedEnv, &volumes, &volumeMounts, &needCodeExecIsolation)
	if err != nil {
		return nil, err
	}

	volumes = append(volumes, projectedTokenVolume())
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "kagent-token",
		MountPath: "/var/run/secrets/tokens",
	})

	if needsSRTSettings(manifestCtx.agent, sandboxCfg) {
		sharedEnv = append(sharedEnv, corev1.EnvVar{
			Name:  env.KagentSRTSettingsPath.Name(),
			Value: env.KagentSRTSettingsPath.DefaultValue(),
		})
	}

	envVars := append([]corev1.EnvVar{}, manifestCtx.deployment.Env...)
	envVars = append(envVars, sharedEnv...)

	return &podRuntimeInputs{
		initContainers:  initContainers,
		envVars:         envVars,
		volumes:         volumes,
		volumeMounts:    volumeMounts,
		securityContext: buildContainerSecurityContext(manifestCtx.deployment.SecurityContext, needCodeExecIsolation),
	}, nil
}

func needsSRTSettings(agent v1alpha2.AgentObject, sandboxCfg *v1alpha2.SandboxConfig) bool {
	spec := agent.GetAgentSpec()
	if spec.Type == v1alpha2.AgentType_BYO {
		return sandboxCfg != nil
	}
	if spec.Skills != nil {
		return true
	}
	return spec.Declarative != nil &&
		spec.Declarative.ExecuteCodeBlocks != nil &&
		*spec.Declarative.ExecuteCodeBlocks
}

func buildSRTSettingsJSON(sandboxCfg *v1alpha2.SandboxConfig) ([]byte, error) {
	allowedDomains := []string{}
	if sandboxCfg != nil && sandboxCfg.Network != nil {
		allowedDomains = append(allowedDomains, sandboxCfg.Network.AllowedDomains...)
	}

	return json.Marshal(map[string]any{
		"network": map[string]any{
			"allowedDomains": allowedDomains,
			"deniedDomains":  []string{},
		},
		"filesystem": map[string]any{
			"denyRead":   []string{},
			"allowWrite": []string{".", "/tmp"},
			"denyWrite":  []string{},
		},
	})
}

func collectSharedEnv(agent v1alpha2.AgentObject) []corev1.EnvVar {
	sharedEnv := make([]corev1.EnvVar, 0, 8)
	sharedEnv = append(sharedEnv, collectOtelEnvFromProcess()...)
	sharedEnv = append(sharedEnv,
		corev1.EnvVar{
			Name: env.KagentNamespace.Name(),
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
		corev1.EnvVar{
			Name:  env.KagentName.Name(),
			Value: agent.GetName(),
		},
		corev1.EnvVar{
			Name:  env.KagentURL.Name(),
			Value: fmt.Sprintf("http://%s.%s:8083", utils.GetControllerName(), utils.GetResourceNamespace()),
		},
	)
	return sharedEnv
}

func buildSkillsRuntime(
	manifestCtx manifestContext,
	sharedEnv *[]corev1.EnvVar,
	volumes *[]corev1.Volume,
	volumeMounts *[]corev1.VolumeMount,
	needCodeExecIsolation *bool,
) ([]corev1.Container, error) {
	spec := manifestCtx.agent.GetAgentSpec()
	if spec.Skills == nil {
		return nil, nil
	}

	skills := spec.Skills.Refs
	gitRefs := spec.Skills.GitRefs
	if len(skills) == 0 && len(gitRefs) == 0 {
		return nil, nil
	}

	*needCodeExecIsolation = true
	*sharedEnv = append(*sharedEnv, corev1.EnvVar{
		Name:  env.KagentSkillsFolder.Name(),
		Value: "/skills",
	})
	*volumes = append(*volumes, corev1.Volume{
		Name: "kagent-skills",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})
	*volumeMounts = append(*volumeMounts, corev1.VolumeMount{
		Name:      "kagent-skills",
		MountPath: "/skills",
		ReadOnly:  true,
	})

	var initResources *corev1.ResourceRequirements
	var initEnv []corev1.EnvVar
	if spec.Skills.InitContainer != nil {
		if spec.Skills.InitContainer.Resources != nil {
			initResources = spec.Skills.InitContainer.Resources.DeepCopy()
		}
		initEnv = append(initEnv, spec.Skills.InitContainer.Env...)
	}

	container, skillsVolumes, err := buildSkillsInitContainer(
		gitRefs,
		spec.Skills.GitAuthSecretRef,
		skills,
		spec.Skills.InsecureSkipVerify,
		manifestCtx.deployment.SecurityContext,
		initEnv,
		getDefaultResources(initResources),
		spec.Skills.ImagePullSecrets,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build skills init container: %w", err)
	}

	*volumes = append(*volumes, skillsVolumes...)
	return container, nil
}

func projectedTokenVolume() corev1.Volume {
	return corev1.Volume{
		Name: "kagent-token",
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{{
					ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
						Audience:          "kagent",
						ExpirationSeconds: new(int64(3600)),
						Path:              "kagent-token",
					},
				}},
			},
		},
	}
}

func buildContainerSecurityContext(
	base *corev1.SecurityContext,
	needCodeExecIsolation bool,
) *corev1.SecurityContext {
	if base != nil {
		securityContext := base.DeepCopy()
		if needCodeExecIsolation && !allowPrivilegeEscalationExplicitlyFalse(securityContext) {
			securityContext.Privileged = new(true)
		}
		return securityContext
	}

	if !needCodeExecIsolation {
		return nil
	}

	return &corev1.SecurityContext{Privileged: new(true)}
}

func buildPodTemplate(
	manifestCtx manifestContext,
	runtimeInputs *podRuntimeInputs,
	configHash uint64,
) corev1.PodTemplateSpec {
	dep := manifestCtx.deployment
	podTemplateAnnotations := maps.Clone(dep.Annotations)
	if podTemplateAnnotations == nil {
		podTemplateAnnotations = map[string]string{}
	}
	podTemplateAnnotations["kagent.dev/config-hash"] = fmt.Sprintf("%d", configHash)

	probeConf := getRuntimeProbeConfig(agentRuntime(manifestCtx.agent.GetAgentSpec()))

	var cmd []string
	if dep.Cmd != "" {
		cmd = []string{dep.Cmd}
	}

	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      manifestCtx.podLabels(),
			Annotations: podTemplateAnnotations,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: *dep.ServiceAccountName,
			ImagePullSecrets:   dep.ImagePullSecrets,
			SecurityContext:    dep.PodSecurityContext,
			InitContainers:     runtimeInputs.initContainers,
			Containers: append([]corev1.Container{{
				Name:            "kagent",
				Image:           dep.Image,
				ImagePullPolicy: dep.ImagePullPolicy,
				Command:         cmd,
				Args:            dep.Args,
				Ports:           []corev1.ContainerPort{{Name: "http", ContainerPort: dep.Port}},
				Resources:       dep.Resources,
				Env:             runtimeInputs.envVars,
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/.well-known/agent-card.json",
							Port: intstr.FromString("http"),
						},
					},
					InitialDelaySeconds: probeConf.InitialDelaySeconds,
					TimeoutSeconds:      probeConf.TimeoutSeconds,
					PeriodSeconds:       probeConf.PeriodSeconds,
				},
				SecurityContext: runtimeInputs.securityContext,
				VolumeMounts:    runtimeInputs.volumeMounts,
			}}, dep.ExtraContainers...),
			Volumes:      runtimeInputs.volumes,
			Tolerations:  dep.Tolerations,
			Affinity:     dep.Affinity,
			NodeSelector: dep.NodeSelector,
		},
	}
}

func agentRuntime(spec *v1alpha2.AgentSpec) v1alpha2.DeclarativeRuntime {
	runtime := v1alpha2.DeclarativeRuntime_Python
	if spec.Type == v1alpha2.AgentType_Declarative && spec.Declarative != nil && spec.Declarative.Runtime != "" {
		runtime = spec.Declarative.Runtime
	}
	return runtime
}

func (a *adkApiTranslator) buildWorkloadObjects(
	ctx context.Context,
	manifestCtx manifestContext,
	podTemplate corev1.PodTemplateSpec,
) ([]client.Object, error) {
	if manifestCtx.runInSandbox() {
		sbObjs, err := a.sandboxBackend.BuildSandbox(ctx, sandboxbackend.BuildInput{
			Agent:       manifestCtx.agent,
			PodTemplate: podTemplate,
		})
		if err != nil {
			return nil, fmt.Errorf("build sandbox workload: %w", err)
		}
		return sbObjs, nil
	}

	return []client.Object{
		&appsv1.Deployment{
			TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
			ObjectMeta: manifestCtx.objectMeta(),
			Spec: appsv1.DeploymentSpec{
				Replicas: manifestCtx.deployment.Replicas,
				Strategy: appsv1.DeploymentStrategy{
					Type: appsv1.RollingUpdateDeploymentStrategyType,
					RollingUpdate: &appsv1.RollingUpdateDeployment{
						MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 0},
						MaxSurge:       &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
					},
				},
				Selector: &metav1.LabelSelector{MatchLabels: manifestCtx.selectorLabels},
				Template: podTemplate,
			},
		},
		&corev1.Service{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
			ObjectMeta: manifestCtx.objectMeta(),
			Spec: corev1.ServiceSpec{
				Selector: manifestCtx.selectorLabels,
				Ports: []corev1.ServicePort{{
					Name:       "http",
					Port:       manifestCtx.deployment.Port,
					TargetPort: intstr.FromInt(int(manifestCtx.deployment.Port)),
				}},
				Type: corev1.ServiceTypeClusterIP,
			},
		},
	}, nil
}

func (a *adkApiTranslator) setManifestOwnerReferences(
	agent v1alpha2.AgentObject,
	manifest []client.Object,
) error {
	for _, obj := range manifest {
		if err := controllerutil.SetControllerReference(agent, obj, a.kube.Scheme()); err != nil {
			return err
		}
	}
	return nil
}

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/controller/translator/labels"
	"github.com/kagent-dev/kagent/go/core/internal/skillsinit"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// configHashAnnotation is set on the agent pod template so a change to
// the serialized config Secret (including tool URLs and ModelConfig/RMS
// Secret rotations folded in via Status hashes) rolls the pod. It is the
// shared consts.ConfigHashAnnotation key (the substrate backend mirrors
// the same annotation onto generated ActorTemplates), defined once there.
const configHashAnnotation = consts.ConfigHashAnnotation

type manifestContext struct {
	agent          *v1alpha3.SandboxAgent
	deployment     *resolvedDeployment
	selectorLabels map[string]string
}

type configSecretInputs struct {
	secret  *corev1.Secret
	volumes []corev1.Volume
	mounts  []corev1.VolumeMount
	// hashInput is the byte payload that should be folded into the pod
	// template's config-hash annotation. Hashing is done by the caller once
	// all rollout-relevant inputs (including the skills-init ConfigMap) are
	// known.
	hashInput configHashInput
}

type configHashInput struct {
	agentCfg   []byte
	agentCard  []byte
	secretData []byte
}

type podRuntimeInputs struct {
	initContainers  []corev1.Container
	envVars         []corev1.EnvVar
	volumes         []corev1.Volume
	volumeMounts    []corev1.VolumeMount
	securityContext *corev1.SecurityContext
	// skillsInitConfigMap is the ConfigMap (when skills are configured) that
	// carries the JSON configuration consumed by the skills-init binary. It
	// is added to AgentOutputs.Manifest and content-hashed into the pod
	// template annotations so changes trigger a rollout.
	skillsInitConfigMap *corev1.ConfigMap
}

func getDefaultResources(spec *corev1.ResourceRequirements) corev1.ResourceRequirements {
	if spec != nil {
		return *spec
	}
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

func (a *adkApiTranslator) BuildManifest(
	ctx context.Context,
	agent *v1alpha3.SandboxAgent,
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
	// The translator is the single writer of the config Secret for every workload mode; sandbox
	// backends contribute their config (e.g. session_db_url) upstream in CompileAgent, and their
	// ActorTemplates reference this Secret by the agent's stable name.
	outputs.Manifest = append(outputs.Manifest, configSecret.secret)

	podRuntime, err := buildPodRuntime(manifestCtx, inputs.Sandbox, configSecret.volumes, configSecret.mounts)
	if err != nil {
		return nil, err
	}

	var skillsInitCfg []byte
	if podRuntime.skillsInitConfigMap != nil {
		outputs.Manifest = append(outputs.Manifest, podRuntime.skillsInitConfigMap)
		// Folded into the same rollout-trigger hash as the rest of the pod
		// config, since the PodSpec only names the ConfigMap, so Kubernetes
		// wouldn't otherwise restart the pod when its rendered config changes.
		skillsInitCfg = []byte(podRuntime.skillsInitConfigMap.Data[skillsinit.ConfigMapKey])
	}
	var configHash uint64
	if h := configSecret.hashInput; h.agentCfg != nil || h.agentCard != nil || h.secretData != nil || skillsInitCfg != nil {
		configHash = computeConfigHash(h.agentCfg, h.agentCard, h.secretData, skillsInitCfg)
	}

	podTemplate := buildPodTemplate(manifestCtx, podRuntime, configHash)

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

func newManifestContext(agent *v1alpha3.SandboxAgent, dep *resolvedDeployment) manifestContext {
	return manifestContext{
		agent:      agent,
		deployment: dep,
		selectorLabels: map[string]string{
			"app":    labels.ManagedByKagent,
			"kagent": agent.GetName(),
		},
	}
}

func (m manifestContext) podLabels() map[string]string {
	return maps.Clone(m.selectorLabels)
}

// objectMeta returns the metadata shared by every object emitted for an agent. The
// annotations inherited from the agent resource are cloned, so that builders which
// extend them never mutate the agent object held by the client cache.
func (m manifestContext) objectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:        m.agent.GetName(),
		Namespace:   m.agent.GetNamespace(),
		Annotations: maps.Clone(m.agent.GetAnnotations()),
		Labels:      m.podLabels(),
	}
}

func (a *adkApiTranslator) buildConfigSecret(
	manifestCtx manifestContext,
	cfg *adk.AgentConfig,
	sandboxCfg *v1alpha3.SandboxConfig,
	card *a2a.AgentCard,
	modelConfigSecretHashBytes []byte,
) (*configSecretInputs, error) {
	cfgJSON := ""
	agentCard := ""
	srtSettingsJSON := ""
	var hashInput configHashInput
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
		cardJSON, err := json.Marshal(card)
		if err != nil {
			return nil, err
		}
		agentCard = string(cardJSON)
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
		hashInput = configHashInput{
			agentCfg:   []byte(cfgJSON),
			agentCard:  []byte(agentCard),
			secretData: hashData,
		}
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
		volumes:   volumes,
		mounts:    mounts,
		hashInput: hashInput,
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

func buildPodRuntime(
	manifestCtx manifestContext,
	sandboxCfg *v1alpha3.SandboxConfig,
	secretVolumes []corev1.Volume,
	secretMounts []corev1.VolumeMount,
) (*podRuntimeInputs, error) {
	sharedEnv := collectSharedEnv(manifestCtx.agent)

	volumes := append([]corev1.Volume{}, secretVolumes...)
	volumeMounts := append([]corev1.VolumeMount{}, secretMounts...)

	needCodeExecIsolation := false
	initContainers, skillsInitCM, err := buildSkillsRuntime(manifestCtx, &sharedEnv, &volumes, &volumeMounts, &needCodeExecIsolation)
	if err != nil {
		return nil, err
	}

	if needsSRTSettings(manifestCtx.agent, sandboxCfg) {
		sharedEnv = append(sharedEnv, corev1.EnvVar{
			Name:  env.KagentSRTSettingsPath.Name(),
			Value: env.KagentSRTSettingsPath.DefaultValue(),
		})
	}

	envVars := mergeEnv(manifestCtx.deployment.Env, sharedEnv)

	return &podRuntimeInputs{
		initContainers:      initContainers,
		envVars:             envVars,
		volumes:             volumes,
		volumeMounts:        volumeMounts,
		securityContext:     buildContainerSecurityContext(nil, needCodeExecIsolation),
		skillsInitConfigMap: skillsInitCM,
	}, nil
}

// mergeEnv appends shared entries whose name is not already set in userEnv,
// so a user-supplied env var always wins over a controller-injected one with
// the same name instead of being silently overwritten by append order.
func mergeEnv(userEnv, sharedEnv []corev1.EnvVar) []corev1.EnvVar {
	set := make(map[string]bool, len(userEnv))
	for _, e := range userEnv {
		set[e.Name] = true
	}
	merged := append([]corev1.EnvVar{}, userEnv...)
	for _, e := range sharedEnv {
		if !set[e.Name] {
			merged = append(merged, e)
		}
	}
	return merged
}

func needsSRTSettings(agent *v1alpha3.SandboxAgent, sandboxCfg *v1alpha3.SandboxConfig) bool {
	spec := agent.GetAgentSpec()
	if spec.Type == v1alpha3.AgentType_BYO {
		return sandboxCfg != nil
	}
	return spec.Skills != nil
}

func buildSRTSettingsJSON(sandboxCfg *v1alpha3.SandboxConfig) ([]byte, error) {
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

func collectSharedEnv(agent *v1alpha3.SandboxAgent) []corev1.EnvVar {
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
		corev1.EnvVar{
			Name:  env.KagentGRPCURL.Name(),
			Value: fmt.Sprintf("%s.%s:8084", utils.GetControllerName(), utils.GetResourceNamespace()),
		},
	)
	if uiURL := env.KagentUIURL.Get(); uiURL != "" {
		sharedEnv = append(sharedEnv, corev1.EnvVar{
			Name:  env.KagentUIURL.Name(),
			Value: uiURL,
		})
	}
	return sharedEnv
}

func buildSkillsRuntime(
	manifestCtx manifestContext,
	sharedEnv *[]corev1.EnvVar,
	volumes *[]corev1.Volume,
	volumeMounts *[]corev1.VolumeMount,
	needCodeExecIsolation *bool,
) ([]corev1.Container, *corev1.ConfigMap, error) {
	spec := manifestCtx.agent.GetAgentSpec()
	if spec.Skills == nil {
		return nil, nil, nil
	}

	skills := spec.Skills.Refs
	gitRefs := spec.Skills.GitRefs
	s3Refs := spec.Skills.S3Refs
	if len(skills) == 0 && len(gitRefs) == 0 && len(s3Refs) == 0 {
		return nil, nil, nil
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

	container, skillsVolumes, configMap, err := buildSkillsInitContainer(
		manifestCtx.agent.GetName(),
		manifestCtx.agent.GetNamespace(),
		gitRefs,
		spec.Skills.GitAuthSecretRef,
		skills,
		spec.Skills.InsecureSkipVerify,
		nil,
		initEnv,
		getDefaultResources(initResources),
		spec.Skills.ImagePullSecrets,
		s3Refs,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build skills init container: %w", err)
	}

	*volumes = append(*volumes, skillsVolumes...)
	return container, configMap, nil
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
	podTemplateAnnotations := map[string]string{}
	podTemplateAnnotations[configHashAnnotation] = fmt.Sprintf("%d", configHash)

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
			InitContainers: runtimeInputs.initContainers,
			Containers: []corev1.Container{{
				Name:            "kagent",
				Image:           dep.Image,
				Command:         cmd,
				Args:            dep.Args,
				Env:             runtimeInputs.envVars,
				SecurityContext: runtimeInputs.securityContext,
				VolumeMounts:    runtimeInputs.volumeMounts,
			}},
			Volumes: runtimeInputs.volumes,
		},
	}
}

func (a *adkApiTranslator) buildWorkloadObjects(
	ctx context.Context,
	manifestCtx manifestContext,
	podTemplate corev1.PodTemplateSpec,
) ([]client.Object, error) {
	sbObjs, err := a.sandboxBackend.BuildSandbox(ctx, sandboxbackend.BuildInput{
		Agent:       manifestCtx.agent,
		PodTemplate: podTemplate,
	})
	if err != nil {
		return nil, fmt.Errorf("build sandbox workload: %w", err)
	}
	return sbObjs, nil
}

func (a *adkApiTranslator) setManifestOwnerReferences(
	agent *v1alpha3.SandboxAgent,
	manifest []client.Object,
) error {
	for _, obj := range manifest {
		if err := controllerutil.SetControllerReference(agent, obj, a.kube.Scheme()); err != nil {
			return err
		}
	}
	return nil
}

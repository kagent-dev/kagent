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
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	corev1 "k8s.io/api/core/v1"
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
	// all rollout-relevant inputs are known.
	hashInput configHashInput
}

type configHashInput struct {
	agentCfg   []byte
	agentCard  []byte
	secretData []byte
}

type podRuntimeInputs struct {
	envVars      []corev1.EnvVar
	volumes      []corev1.Volume
	volumeMounts []corev1.VolumeMount
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

	configSecret, err := a.buildConfigSecret(manifestCtx, inputs.Config, inputs.AgentCard, inputs.SecretHashBytes)
	if err != nil {
		return nil, err
	}
	// The translator is the single writer of the config Secret for every workload mode; sandbox
	// backends contribute their config (e.g. session_db_url) upstream in CompileAgent, and their
	// ActorTemplates reference this Secret by the agent's stable name.
	outputs.Manifest = append(outputs.Manifest, configSecret.secret)

	podRuntime := buildPodRuntime(manifestCtx, configSecret.volumes, configSecret.mounts)

	var configHash uint64
	if h := configSecret.hashInput; h.agentCfg != nil || h.agentCard != nil || h.secretData != nil {
		configHash = computeConfigHash(h.agentCfg, h.agentCard, h.secretData)
	}

	podTemplate := buildPodTemplate(manifestCtx, podRuntime, configHash)

	workloadObjects, err := a.buildWorkloadObjects(ctx, manifestCtx, podTemplate, configSecret.secret)
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
	card *a2a.AgentCard,
	modelConfigSecretHashBytes []byte,
) (*configSecretInputs, error) {
	cfgJSON := ""
	agentCard := ""
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
	if cfg != nil {
		secretData := modelConfigSecretHashBytes
		if secretData == nil {
			secretData = []byte{}
		}
		hashInput = configHashInput{
			agentCfg:   []byte(cfgJSON),
			agentCard:  []byte(agentCard),
			secretData: secretData,
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
			StringData: buildConfigSecretData(cfgJSON, agentCard),
		},
		volumes:   volumes,
		mounts:    mounts,
		hashInput: hashInput,
	}, nil
}

func buildConfigSecretData(cfgJSON, agentCard string) map[string]string {
	return map[string]string{
		"config.json":     cfgJSON,
		"agent-card.json": agentCard,
	}
}

func buildPodRuntime(
	manifestCtx manifestContext,
	secretVolumes []corev1.Volume,
	secretMounts []corev1.VolumeMount,
) *podRuntimeInputs {
	sharedEnv := collectSharedEnv(manifestCtx.agent)

	volumes := append([]corev1.Volume{}, secretVolumes...)
	volumeMounts := append([]corev1.VolumeMount{}, secretMounts...)

	envVars := append([]corev1.EnvVar{}, manifestCtx.deployment.Env...)
	envVars = append(envVars, sharedEnv...)

	return &podRuntimeInputs{envVars: envVars, volumes: volumes, volumeMounts: volumeMounts}
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
			Containers: []corev1.Container{{
				Name:         "kagent",
				Image:        dep.Image,
				Command:      cmd,
				Args:         dep.Args,
				Env:          runtimeInputs.envVars,
				VolumeMounts: runtimeInputs.volumeMounts,
			}},
			Volumes: runtimeInputs.volumes,
		},
	}
}

func (a *adkApiTranslator) buildWorkloadObjects(
	ctx context.Context,
	manifestCtx manifestContext,
	podTemplate corev1.PodTemplateSpec,
	configSecret *corev1.Secret,
) ([]client.Object, error) {
	sbObjs, err := a.sandboxBackend.BuildSandbox(ctx, sandboxbackend.BuildInput{
		Agent:        manifestCtx.agent,
		PodTemplate:  podTemplate,
		ConfigSecret: configSecret,
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

package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func Test_AdkApiTranslator_Skills(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	namespace := "default"
	modelName := "test-model"

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      modelName,
			Namespace: namespace,
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	}

	defaultModel := types.NamespacedName{
		Namespace: namespace,
		Name:      modelName,
	}

	tests := []struct {
		name  string
		agent *v1alpha2.Agent
		// assertions
		wantSkillsInit     bool
		wantSkillsVolume   bool
		wantContainsBranch string
		wantContainsCommit string
		wantContainsPath   string
		wantContainsKrane  bool
		wantAuthVolume     bool
		wantAuthSecretName string
		wantScriptContains []string
	}{
		{
			name: "no skills - no init containers",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-no-skills", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
				},
			},
			wantSkillsInit:   false,
			wantSkillsVolume: false,
		},
		{
			name: "only OCI skills - unified init container with krane",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-oci-only", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						Refs: []string{"ghcr.io/org/skill:v1"},
					},
				},
			},
			wantSkillsInit:    true,
			wantSkillsVolume:  true,
			wantContainsKrane: true,
		},
		{
			name: "only git skills - unified init container with git clone",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-git-only", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						GitRefs: []v1alpha2.GitRepo{
							{
								URL: "https://github.com/org/my-skills",
								Ref: "v1.0.0",
							},
						},
					},
				},
			},
			wantSkillsInit:     true,
			wantSkillsVolume:   true,
			wantContainsBranch: "v1.0.0",
		},
		{
			name: "both OCI and git skills - single unified init container",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-both", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						Refs: []string{"ghcr.io/org/skill:v1"},
						GitRefs: []v1alpha2.GitRepo{
							{
								URL: "https://github.com/org/my-skills",
								Ref: "main",
							},
						},
					},
				},
			},
			wantSkillsInit:    true,
			wantSkillsVolume:  true,
			wantContainsKrane: true,
		},
		{
			name: "git skill with commit SHA",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-commit", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						GitRefs: []v1alpha2.GitRepo{
							{
								URL: "https://github.com/org/my-skills",
								Ref: "abc123def456abc123def456abc123def456abc1",
							},
						},
					},
				},
			},
			wantSkillsInit:     true,
			wantSkillsVolume:   true,
			wantContainsCommit: "abc123def456abc123def456abc123def456abc1",
		},
		{
			name: "git skill with path subdirectory",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-path", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						GitRefs: []v1alpha2.GitRepo{
							{
								URL:  "https://github.com/org/mono-repo",
								Ref:  "main",
								Path: "skills/k8s",
							},
						},
					},
				},
			},
			wantSkillsInit:   true,
			wantSkillsVolume: true,
			wantContainsPath: "skills/k8s",
		},
		{
			name: "git skills with shared auth secret",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-auth", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						GitAuthSecretRef: &corev1.LocalObjectReference{
							Name: "github-token",
						},
						GitRefs: []v1alpha2.GitRepo{
							{
								URL: "https://github.com/org/private-skill",
								Ref: "main",
							},
							{
								URL: "https://github.com/org/another-private-skill",
								Ref: "v1.0.0",
							},
						},
					},
				},
			},
			wantSkillsInit:     true,
			wantSkillsVolume:   true,
			wantAuthVolume:     true,
			wantAuthSecretName: "github-token",
			wantScriptContains: []string{
				"credential.helper",
			},
		},
		{
			name: "git skills with SSH URL and auth secret scans custom host",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-ssh-auth", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						GitAuthSecretRef: &corev1.LocalObjectReference{
							Name: "gitea-ssh-credentials",
						},
						GitRefs: []v1alpha2.GitRepo{
							{
								URL:  "ssh://git@gitea-ssh.gitea:22/gitops/ssh-skills-repo.git",
								Ref:  "main",
								Name: "ssh-skill",
							},
						},
					},
				},
			},
			wantSkillsInit:     true,
			wantSkillsVolume:   true,
			wantAuthVolume:     true,
			wantAuthSecretName: "gitea-ssh-credentials",
			wantScriptContains: []string{
				"ssh-keyscan",
				"gitea-ssh.gitea",
			},
		},
		{
			name: "git skill with custom name",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-custom-name", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						GitRefs: []v1alpha2.GitRepo{
							{
								URL:  "https://github.com/org/my-skills.git",
								Ref:  "main",
								Name: "custom-skill",
							},
						},
					},
				},
			},
			wantSkillsInit:   true,
			wantSkillsVolume: true,
		},
		{
			name: "OCI skills with insecure flag",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-insecure", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						InsecureSkipVerify: true,
						Refs:               []string{"localhost:5000/skill:dev"},
					},
				},
			},
			wantSkillsInit:    true,
			wantSkillsVolume:  true,
			wantContainsKrane: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(modelConfig, tt.agent).
				Build()

			trans := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "", nil)

			outputs, err := translator.TranslateAgent(context.Background(), trans, tt.agent)
			require.NoError(t, err)
			require.NotNil(t, outputs)

			// Find deployment in manifest
			var deployment *appsv1.Deployment
			for _, obj := range outputs.Manifest {
				if d, ok := obj.(*appsv1.Deployment); ok {
					deployment = d
				}
			}
			require.NotNil(t, deployment, "Deployment should be created")

			initContainers := deployment.Spec.Template.Spec.InitContainers

			// Find the unified skills-init container
			var skillsInitContainer *corev1.Container
			for i := range initContainers {
				if initContainers[i].Name == "skills-init" {
					skillsInitContainer = &initContainers[i]
				}
			}

			if tt.wantSkillsInit {
				require.NotNil(t, skillsInitContainer, "skills-init container should exist")
				// There should be exactly one init container
				assert.Len(t, initContainers, 1, "should have exactly one init container")

				// Verify the script is passed via /bin/sh -c
				require.Len(t, skillsInitContainer.Command, 3)
				assert.Equal(t, "/bin/sh", skillsInitContainer.Command[0])
				assert.Equal(t, "-c", skillsInitContainer.Command[1])
				script := skillsInitContainer.Command[2]

				if tt.wantContainsBranch != "" {
					assert.Contains(t, script, tt.wantContainsBranch)
					assert.Contains(t, script, "--branch")
				}

				if tt.wantContainsCommit != "" {
					assert.Contains(t, script, tt.wantContainsCommit)
					assert.Contains(t, script, "git checkout")
				}

				if tt.wantContainsPath != "" {
					assert.Contains(t, script, tt.wantContainsPath)
					assert.Contains(t, script, "mktemp")
				}

				if tt.wantContainsKrane {
					assert.Contains(t, script, "krane export")
				}

				for _, want := range tt.wantScriptContains {
					assert.Contains(t, script, want)
				}

				// Verify /skills volume mount exists
				hasSkillsMount := false
				for _, vm := range skillsInitContainer.VolumeMounts {
					if vm.Name == "kagent-skills" && vm.MountPath == "/skills" {
						hasSkillsMount = true
					}
				}
				assert.True(t, hasSkillsMount, "skills-init container should mount kagent-skills volume")
			} else {
				assert.Nil(t, skillsInitContainer, "skills-init container should not exist")
				assert.Empty(t, initContainers, "should have no init containers")
			}

			// Check skills volume exists
			hasSkillsVolume := false
			for _, v := range deployment.Spec.Template.Spec.Volumes {
				if v.Name == "kagent-skills" {
					hasSkillsVolume = true
					if tt.wantSkillsVolume {
						assert.NotNil(t, v.EmptyDir, "kagent-skills should be an EmptyDir volume")
					}
				}
			}
			if tt.wantSkillsVolume {
				assert.True(t, hasSkillsVolume, "kagent-skills volume should exist")
			} else {
				assert.False(t, hasSkillsVolume, "kagent-skills volume should not exist")
			}

			// Check auth volume
			if tt.wantAuthVolume {
				hasAuthVolume := false
				for _, v := range deployment.Spec.Template.Spec.Volumes {
					if v.Secret != nil && v.Name == "git-auth" {
						hasAuthVolume = true
						assert.Equal(t, tt.wantAuthSecretName, v.Secret.SecretName, "auth volume should reference the correct secret")
					}
				}
				assert.True(t, hasAuthVolume, "git-auth volume should exist")

				// Verify skills-init container has auth volume mount
				require.NotNil(t, skillsInitContainer)
				hasAuthMount := false
				for _, vm := range skillsInitContainer.VolumeMounts {
					if vm.Name == "git-auth" && vm.MountPath == "/git-auth" {
						hasAuthMount = true
					}
				}
				assert.True(t, hasAuthMount, "skills-init container should mount auth secret")
			}

			// Verify insecure flag for OCI skills
			if tt.agent.Spec.Skills != nil && tt.agent.Spec.Skills.InsecureSkipVerify {
				require.NotNil(t, skillsInitContainer)
				script := skillsInitContainer.Command[2]
				assert.Contains(t, script, "--insecure")
			}
		})
	}
}

func Test_AdkApiTranslator_SkillsImagePullSecrets(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	namespace := "default"
	modelName := "test-model"

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      modelName,
			Namespace: namespace,
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	}

	defaultModel := types.NamespacedName{
		Namespace: namespace,
		Name:      modelName,
	}

	tests := []struct {
		name               string
		agent              *v1alpha2.Agent
		wantDockerAuthInit bool
		wantInitCount      int
	}{
		{
			name: "OCI skills without imagePullSecrets - single init container",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-no-pull-secret", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						Refs: []string{"ghcr.io/org/skill:v1"},
					},
				},
			},
			wantDockerAuthInit: false,
			wantInitCount:      1,
		},
		{
			name: "OCI skills with single imagePullSecret - two init containers",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-one-pull-secret", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						Refs:             []string{"docker.artifactory.example.com/org/skill:v1"},
						ImagePullSecrets: []corev1.LocalObjectReference{{Name: "registry-credentials"}},
					},
				},
			},
			wantDockerAuthInit: true,
			wantInitCount:      2,
		},
		{
			name: "OCI skills with multiple imagePullSecrets - two init containers merging all auths",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-multi-pull-secrets", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						Refs: []string{
							"docker.artifactory.example.com/org/skill-a:v1",
							"acr.azurecr.io/org/skill-b:v2",
						},
						ImagePullSecrets: []corev1.LocalObjectReference{
							{Name: "artifactory-creds"},
							{Name: "acr-creds"},
						},
					},
				},
			},
			wantDockerAuthInit: true,
			wantInitCount:      2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(modelConfig, tt.agent).
				Build()

			trans := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "", nil)

			outputs, err := translator.TranslateAgent(context.Background(), trans, tt.agent)
			require.NoError(t, err)
			require.NotNil(t, outputs)

			var deployment *appsv1.Deployment
			for _, obj := range outputs.Manifest {
				if d, ok := obj.(*appsv1.Deployment); ok {
					deployment = d
				}
			}
			require.NotNil(t, deployment, "Deployment should be created")

			initContainers := deployment.Spec.Template.Spec.InitContainers
			assert.Len(t, initContainers, tt.wantInitCount, "unexpected number of init containers")

			// Find the skills-init container
			var skillsInitContainer *corev1.Container
			for i := range initContainers {
				if initContainers[i].Name == "skills-init" {
					skillsInitContainer = &initContainers[i]
				}
			}
			require.NotNil(t, skillsInitContainer, "skills-init container should always exist")

			if tt.wantDockerAuthInit {
				// Verify docker-auth-init container exists and is the first init container
				require.True(t, len(initContainers) >= 2, "should have at least 2 init containers")
				assert.Equal(t, "docker-auth-init", initContainers[0].Name, "docker-auth-init should be first")

				dockerAuthInit := &initContainers[0]

				// Verify merge script uses jq and writes to the correct path
				require.Len(t, dockerAuthInit.Command, 3)
				mergeScript := dockerAuthInit.Command[2]
				assert.Contains(t, mergeScript, "jq")
				assert.Contains(t, mergeScript, ".dockerconfigjson")
				assert.Contains(t, mergeScript, "/docker-config-out/config.json")

				// Verify docker-config-out volume mount exists on docker-auth-init
				hasConfigOut := false
				for _, vm := range dockerAuthInit.VolumeMounts {
					if vm.Name == "kagent-docker-config" && vm.MountPath == "/docker-config-out" {
						hasConfigOut = true
					}
				}
				assert.True(t, hasConfigOut, "docker-auth-init should mount kagent-docker-config at /docker-config-out")

				// Verify pull secret volumes and mounts are present on docker-auth-init
				require.NotNil(t, tt.agent.Spec.Skills)
				for _, ps := range tt.agent.Spec.Skills.ImagePullSecrets {
					volName := "pull-secret-" + ps.Name

					// Volume on deployment
					hasPullSecretVol := false
					for _, v := range deployment.Spec.Template.Spec.Volumes {
						if v.Name == volName && v.Secret != nil && v.Secret.SecretName == ps.Name {
							hasPullSecretVol = true
						}
					}
					assert.True(t, hasPullSecretVol, "pull-secret volume %q should exist", volName)

					// Mount on docker-auth-init
					hasPullSecretMount := false
					for _, vm := range dockerAuthInit.VolumeMounts {
						if vm.Name == volName && vm.MountPath == "/docker-secrets/"+ps.Name && vm.ReadOnly {
							hasPullSecretMount = true
						}
					}
					assert.True(t, hasPullSecretMount, "docker-auth-init should mount pull-secret %q", volName)

					// Merge script references this secret
					assert.Contains(t, mergeScript, "/docker-secrets/"+ps.Name+"/.dockerconfigjson")
				}

				// Verify shared EmptyDir volume exists
				hasDockerConfigVol := false
				for _, v := range deployment.Spec.Template.Spec.Volumes {
					if v.Name == "kagent-docker-config" {
						hasDockerConfigVol = true
						assert.NotNil(t, v.EmptyDir, "kagent-docker-config should be an EmptyDir volume")
					}
				}
				assert.True(t, hasDockerConfigVol, "kagent-docker-config EmptyDir volume should exist")

				// Verify skills-init mounts the docker config and has DOCKER_CONFIG env
				hasDockerMount := false
				for _, vm := range skillsInitContainer.VolumeMounts {
					if vm.Name == "kagent-docker-config" && vm.MountPath == "/.kagent/.docker" && vm.ReadOnly {
						hasDockerMount = true
					}
				}
				assert.True(t, hasDockerMount, "skills-init should mount kagent-docker-config at /.kagent/.docker")

				hasDockerConfigEnv := false
				for _, e := range skillsInitContainer.Env {
					if e.Name == "DOCKER_CONFIG" && e.Value == "/.kagent/.docker" {
						hasDockerConfigEnv = true
					}
				}
				assert.True(t, hasDockerConfigEnv, "skills-init should have DOCKER_CONFIG env var pointing to /.kagent/.docker")
			} else {
				// No imagePullSecrets: no docker-auth-init, no DOCKER_CONFIG env, no docker config volumes
				for _, c := range initContainers {
					assert.NotEqual(t, "docker-auth-init", c.Name, "docker-auth-init should not exist without imagePullSecrets")
				}
				for _, e := range skillsInitContainer.Env {
					assert.NotEqual(t, "DOCKER_CONFIG", e.Name, "DOCKER_CONFIG env should not be set without imagePullSecrets")
				}
				for _, v := range deployment.Spec.Template.Spec.Volumes {
					assert.NotEqual(t, "kagent-docker-config", v.Name, "kagent-docker-config volume should not exist")
				}
			}
		})
	}
}

func Test_AdkApiTranslator_SkillsConfigurableImage(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	namespace := "default"
	modelName := "test-model"

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      modelName,
			Namespace: namespace,
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	}

	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-custom-image", Namespace: namespace},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "test",
				ModelConfig:   modelName,
			},
			Skills: &v1alpha2.SkillForAgent{
				GitRefs: []v1alpha2.GitRepo{
					{
						URL: "https://github.com/org/my-skills",
						Ref: "main",
					},
				},
			},
		},
	}

	// Override the default skills init image config
	originalConfig := translator.DefaultSkillsInitImageConfig
	translator.DefaultSkillsInitImageConfig = translator.ImageConfig{
		Registry:   "custom-registry",
		Repository: "skills-init",
		Tag:        "latest",
	}
	defer func() { translator.DefaultSkillsInitImageConfig = originalConfig }()

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(modelConfig, agent).
		Build()

	defaultModel := types.NamespacedName{
		Namespace: namespace,
		Name:      modelName,
	}

	trans := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "", nil)
	outputs, err := translator.TranslateAgent(context.Background(), trans, agent)
	require.NoError(t, err)

	var deployment *appsv1.Deployment
	for _, obj := range outputs.Manifest {
		if d, ok := obj.(*appsv1.Deployment); ok {
			deployment = d
		}
	}
	require.NotNil(t, deployment)

	var skillsInitContainer *corev1.Container
	for i := range deployment.Spec.Template.Spec.InitContainers {
		if deployment.Spec.Template.Spec.InitContainers[i].Name == "skills-init" {
			skillsInitContainer = &deployment.Spec.Template.Spec.InitContainers[i]
		}
	}
	require.NotNil(t, skillsInitContainer)
	assert.Equal(t, "custom-registry/skills-init:latest", skillsInitContainer.Image)
}

func Test_AdkApiTranslator_SkillsInitContainer(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	namespace := "default"
	modelName := "test-model"

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      modelName,
			Namespace: namespace,
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	}

	defaultModel := types.NamespacedName{
		Namespace: namespace,
		Name:      modelName,
	}

	tests := []struct {
		name                 string
		agent                *v1alpha2.Agent
		wantResources        corev1.ResourceRequirements
		wantEnvContains      []string // env var names expected on the init container
		wantEnvNotContains   []string // env var names that must NOT be on the init container
		wantDefaultResources bool     // expect the default resource values
	}{
		{
			name: "no initContainer - gets default resources and default securityContext",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-defaults", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						Refs: []string{"ghcr.io/org/skill:v1"},
					},
				},
			},
			wantDefaultResources: true,
		},
		{
			name: "custom resources on initContainer",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-custom-resources", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						Refs: []string{"ghcr.io/org/skill:v1"},
						InitContainer: &v1alpha2.SkillsInitContainer{
							Resources: &corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
			},
			wantResources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			},
		},
		{
			name: "custom env on initContainer",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-custom-env", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						Refs: []string{"ghcr.io/org/skill:v1"},
						InitContainer: &v1alpha2.SkillsInitContainer{
							Env: []corev1.EnvVar{
								{Name: "INIT_CUSTOM", Value: "init-value"},
							},
						},
					},
				},
			},
			wantDefaultResources: true,
			wantEnvContains:      []string{"INIT_CUSTOM"},
		},
		{
			name: "both resources and env on initContainer",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-both-overrides", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
					},
					Skills: &v1alpha2.SkillForAgent{
						Refs: []string{"ghcr.io/org/skill:v1"},
						InitContainer: &v1alpha2.SkillsInitContainer{
							Resources: &corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
							Env: []corev1.EnvVar{
								{Name: "INIT_CUSTOM", Value: "init-value"},
							},
						},
					},
				},
			},
			wantResources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("100m"),
				},
			},
			wantEnvContains: []string{"INIT_CUSTOM"},
		},
		{
			name: "init container does not inherit dep env vars",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "agent-env", Namespace: namespace},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						ModelConfig:   modelName,
						Deployment: &v1alpha2.DeclarativeDeploymentSpec{
							SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
								Env: []corev1.EnvVar{
									{Name: "CUSTOM_VAR", Value: "custom-value"},
								},
							},
						},
					},
					Skills: &v1alpha2.SkillForAgent{
						Refs: []string{"ghcr.io/org/skill:v1"},
					},
				},
			},
			wantDefaultResources: true,
			wantEnvNotContains:   []string{"CUSTOM_VAR"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(modelConfig, tt.agent).
				Build()

			trans := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "", nil)
			outputs, err := translator.TranslateAgent(context.Background(), trans, tt.agent)
			require.NoError(t, err)

			var deployment *appsv1.Deployment
			for _, obj := range outputs.Manifest {
				if d, ok := obj.(*appsv1.Deployment); ok {
					deployment = d
				}
			}
			require.NotNil(t, deployment)

			var initContainer *corev1.Container
			for i := range deployment.Spec.Template.Spec.InitContainers {
				if deployment.Spec.Template.Spec.InitContainers[i].Name == "skills-init" {
					initContainer = &deployment.Spec.Template.Spec.InitContainers[i]
				}
			}
			require.NotNil(t, initContainer, "skills-init container should exist")

			// Check resources
			if tt.wantDefaultResources {
				assert.Equal(t, resource.MustParse("100m"), initContainer.Resources.Requests[corev1.ResourceCPU])
				assert.Equal(t, resource.MustParse("384Mi"), initContainer.Resources.Requests[corev1.ResourceMemory])
				assert.Equal(t, resource.MustParse("2000m"), initContainer.Resources.Limits[corev1.ResourceCPU])
				assert.Equal(t, resource.MustParse("1Gi"), initContainer.Resources.Limits[corev1.ResourceMemory])
			} else {
				assert.Equal(t, tt.wantResources, initContainer.Resources)
			}
			// Check env vars
			envNames := make(map[string]bool)
			for _, e := range initContainer.Env {
				envNames[e.Name] = true
			}
			for _, name := range tt.wantEnvContains {
				assert.True(t, envNames[name], "init container should have env var %s", name)
			}
			for _, name := range tt.wantEnvNotContains {
				assert.False(t, envNames[name], "init container should not have env var %s", name)
			}
		})
	}
}

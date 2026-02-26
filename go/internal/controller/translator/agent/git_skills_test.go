package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	translator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
			wantSkillsInit:   true,
			wantSkillsVolume: true,
			wantAuthVolume:   true,
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

			trans := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "")

			outputs, err := trans.TranslateAgent(context.Background(), tt.agent)
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
						assert.Equal(t, "github-token", v.Secret.SecretName, "auth volume should reference the correct secret")
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

				// Verify script contains credential helper setup
				script := skillsInitContainer.Command[2]
				assert.Contains(t, script, "credential.helper")
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

	trans := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "")
	outputs, err := trans.TranslateAgent(context.Background(), agent)
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

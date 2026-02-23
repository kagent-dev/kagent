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

func Test_AdkApiTranslator_GitSkills(t *testing.T) {
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
		wantGitInit        bool
		wantOCIInit        bool
		wantSkillsVolume   bool
		wantGitImage       string
		wantContainsBranch string
		wantContainsCommit string
		wantContainsPath   string
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
			wantGitInit:      false,
			wantOCIInit:      false,
			wantSkillsVolume: false,
		},
		{
			name: "only OCI skills - no git init container",
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
			wantGitInit:      false,
			wantOCIInit:      true,
			wantSkillsVolume: true,
		},
		{
			name: "only git skills - git init container, no OCI init",
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
			wantGitInit:        true,
			wantOCIInit:        false,
			wantSkillsVolume:   true,
			wantGitImage:       "alpine/git:2.47.2",
			wantContainsBranch: "v1.0.0",
		},
		{
			name: "both OCI and git skills - both init containers",
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
			wantGitInit:      true,
			wantOCIInit:      true,
			wantSkillsVolume: true,
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
			wantGitInit:        true,
			wantOCIInit:        false,
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
			wantGitInit:      true,
			wantOCIInit:      false,
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
			wantGitInit:      true,
			wantOCIInit:      false,
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
			wantGitInit:      true,
			wantOCIInit:      false,
			wantSkillsVolume: true,
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

			// Check git init container
			var gitInitContainer *corev1.Container
			var ociInitContainer *corev1.Container
			for i := range initContainers {
				switch initContainers[i].Name {
				case "git-skills-init":
					gitInitContainer = &initContainers[i]
				case "skills-init":
					ociInitContainer = &initContainers[i]
				}
			}

			if tt.wantGitInit {
				require.NotNil(t, gitInitContainer, "git-skills-init container should exist")

				if tt.wantGitImage != "" {
					assert.Equal(t, tt.wantGitImage, gitInitContainer.Image)
				}

				// Verify the script is passed via /bin/sh -c
				require.Len(t, gitInitContainer.Command, 3)
				assert.Equal(t, "/bin/sh", gitInitContainer.Command[0])
				assert.Equal(t, "-c", gitInitContainer.Command[1])
				script := gitInitContainer.Command[2]

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

				// Verify /skills volume mount exists
				hasSkillsMount := false
				for _, vm := range gitInitContainer.VolumeMounts {
					if vm.Name == "kagent-skills" && vm.MountPath == "/skills" {
						hasSkillsMount = true
					}
				}
				assert.True(t, hasSkillsMount, "git init container should mount kagent-skills volume")
			} else {
				assert.Nil(t, gitInitContainer, "git-skills-init container should not exist")
			}

			if tt.wantOCIInit {
				require.NotNil(t, ociInitContainer, "skills-init container should exist")
			} else {
				assert.Nil(t, ociInitContainer, "skills-init container should not exist")
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

				// Verify git init container has auth volume mount
				require.NotNil(t, gitInitContainer)
				hasAuthMount := false
				for _, vm := range gitInitContainer.VolumeMounts {
					if vm.Name == "git-auth" && vm.MountPath == "/git-auth" {
						hasAuthMount = true
					}
				}
				assert.True(t, hasAuthMount, "git init container should mount auth secret")

				// Verify script contains credential helper setup for all repos
				script := gitInitContainer.Command[2]
				assert.Contains(t, script, "credential.helper")
			}

			// When both exist, verify git init comes before OCI init
			if tt.wantGitInit && tt.wantOCIInit {
				gitIdx := -1
				ociIdx := -1
				for i, c := range initContainers {
					if c.Name == "git-skills-init" {
						gitIdx = i
					}
					if c.Name == "skills-init" {
						ociIdx = i
					}
				}
				assert.Less(t, gitIdx, ociIdx, "git init container should come before OCI init container")
			}
		})
	}
}

func Test_AdkApiTranslator_GitSkillsConfigurableImage(t *testing.T) {
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

	// Override the default git init image
	originalImage := translator.DefaultGitInitImage
	translator.DefaultGitInitImage = "custom-registry/git:latest"
	defer func() { translator.DefaultGitInitImage = originalImage }()

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

	var gitInitContainer *corev1.Container
	for i := range deployment.Spec.Template.Spec.InitContainers {
		if deployment.Spec.Template.Spec.InitContainers[i].Name == "git-skills-init" {
			gitInitContainer = &deployment.Spec.Template.Spec.InitContainers[i]
		}
	}
	require.NotNil(t, gitInitContainer)
	assert.Equal(t, "custom-registry/git:latest", gitInitContainer.Image)
}

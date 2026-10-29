package translator_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	"github.com/kagent-dev/kagent/go/api/agentplugin"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	claudeconfig "github.com/kagent-dev/kagent/go/harness/claude/config"
	codexconfig "github.com/kagent-dev/kagent/go/harness/codex/config"
)

func TestCompileSkillImageVolumes(t *testing.T) {
	const image = "registry.example.com/skills@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, kind := range skillHarnessTypes {
		t.Run(string(kind), func(t *testing.T) {
			c, harness, template := skillCompiler(t, kind, image)
			template.Spec.Skills = []v1alpha3.AgentTemplateSkill{{Name: "review", Source: v1alpha3.ArtifactSource{OCI: image, Path: "skills/review"}}}
			template.Spec.Plugins = []v1alpha3.PluginBundle{{Source: v1alpha3.ArtifactSource{OCI: image, Path: "plugin"}, Skills: []string{"deploy"}}}
			result, err := c.CompileAgent(t.Context(), inlineAgent(harness, template))
			require.NoError(t, err)
			require.Equal(t, []translator.SkillImage{image}, result.SkillImages)
			require.NotContains(t, result.EgressDestinations, "https://registry.example.com:443")
			var resources *agentplugin.Resources
			switch kind {
			case translator.HarnessTypeCodex:
				var cfg codexconfig.Config
				require.NoError(t, json.Unmarshal(result.ConfigJSON, &cfg))
				resources = cfg.SkillResources
			case translator.HarnessTypeClaude:
				var cfg claudeconfig.Config
				require.NoError(t, json.Unmarshal(result.ConfigJSON, &cfg))
				resources = cfg.SkillResources
			default:
				var cfg configv1.ADKConfig
				require.NoError(t, runtimeconfig.Unmarshal(result.ConfigJSON, &cfg))
				compiledResources := runtimeconfig.PluginResources(cfg.Agent.AgentPlugins)
				resources = &compiledResources
			}
			require.NotNil(t, resources)
			require.Equal(t, result.SkillImages[0].MountPath(), resources.Skills[0].Source.Directory)
			require.Equal(t, "skills/review", resources.Skills[0].Source.Path)
			require.Equal(t, resources.Skills[0].Source.Directory, resources.Plugins[0].Source.Directory)
			require.Equal(t, "plugin", resources.Plugins[0].Source.Path)
			require.Equal(t, []string{"deploy"}, resources.Plugins[0].Skills)
			id, err := result.Digest()
			require.NoError(t, err)
			actor, err := substrate.ActorTemplateForRevision(&result.Revision, id)
			require.NoError(t, err)
			require.Len(t, actor.Volumes, 4)
			require.Equal(t, image, actor.Volumes[3].Image.Reference)
			require.Equal(t, actor.Volumes[3].Name, actor.Containers[0].VolumeMounts[3].Name)
			require.Equal(t, resources.Skills[0].Source.Directory, actor.Containers[0].VolumeMounts[3].MountPath)
			// Delivery inputs affect revision identity independently of JSON/provenance.
			changed := result.Revision
			changed.SkillImages = nil
			changedID, err := changed.Digest()
			require.NoError(t, err)
			require.NotEqual(t, id, changedID)
			// The same digest in another repository is a separate source/mount.
			template.Spec.Plugins[0].Source.OCI = strings.Replace(image, "/skills@", "/other@", 1)
			updated, err := c.CompileAgent(t.Context(), inlineAgent(harness, template))
			require.NoError(t, err)
			require.Len(t, updated.SkillImages, 2)
			require.NotEqual(t, updated.SkillImages[0].MountPath(), updated.SkillImages[1].MountPath())
		})
	}
}

var skillHarnessTypes = []translator.HarnessType{translator.HarnessTypeKagent, translator.HarnessTypeCodex, translator.HarnessTypeClaude, translator.HarnessTypeBYO}

// skillCompiler returns a compiler for one Harness type and an AgentTemplate
// with no skills that the caller fills in.
func skillCompiler(t *testing.T, kind translator.HarnessType, image string) (*translator.Compiler, *v1alpha3.Harness, *v1alpha3.AgentTemplate) {
	t.Helper()
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		Workload:  v1alpha3.HarnessWorkload{Image: new(image), Command: []string{"/agent"}},
		Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
	}}
	model := modelConfig()
	model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = "model-auth", "api-key"
	switch kind {
	case translator.HarnessTypeKagent:
		harness.Spec.Kagent = &v1alpha3.KagentHarness{}
	case translator.HarnessTypeCodex:
		harness.Spec.Codex = &v1alpha3.CodexHarness{}
		model.Spec.OpenAI = &v1alpha3.OpenAIConfig{APIFormat: new(v1alpha3.OpenAIAPIFormatResponses)}
	case translator.HarnessTypeClaude:
		harness.Spec.Claude = &v1alpha3.ClaudeHarness{}
		model.Spec.Provider = v1alpha3.ModelProviderAnthropic
	case translator.HarnessTypeBYO:
		harness.Spec.BYO = &v1alpha3.BYOHarness{}
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "model-auth", Namespace: "test"}, Data: map[string][]byte{"api-key": []byte("test")}}
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "skills", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{
		ModelConfig: &v1alpha3.ObjectReference{Name: model.Name}, SystemPrompt: "help",
	}}
	return compiler(t, harness, model, secret), harness, template
}

// The golden actor fetches git and S3 sources before its snapshot, so each
// Harness must report their origins for the golden actor's egress policy.
func TestCompileGoldenEgressDestinations(t *testing.T) {
	const image = "registry.example.com/skills@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	want := []string{"http://git.internal.example:8080", "https://git.example.com:443", "https://objects.example.com:443"}
	for _, kind := range skillHarnessTypes {
		t.Run(string(kind), func(t *testing.T) {
			c, harness, template := skillCompiler(t, kind, image)
			template.Spec.Skills = []v1alpha3.AgentTemplateSkill{
				{Name: "review", Source: v1alpha3.ArtifactSource{Git: &v1alpha3.GitArtifact{URL: "https://git.example.com/skills.git", Commit: strings.Repeat("a", 40)}}},
				{Name: "triage", Source: v1alpha3.ArtifactSource{Git: &v1alpha3.GitArtifact{URL: "https://git.example.com/other.git", Commit: strings.Repeat("b", 40)}}},
				{Name: "summary", Source: v1alpha3.ArtifactSource{OCI: image}},
			}
			template.Spec.Plugins = []v1alpha3.PluginBundle{
				{Source: v1alpha3.ArtifactSource{Git: &v1alpha3.GitArtifact{URL: "http://git.internal.example:8080/plugin.git", Commit: strings.Repeat("c", 40)}}},
				{Source: v1alpha3.ArtifactSource{Bucket: &v1alpha3.BucketArtifact{S3: v1alpha3.S3Object{
					Endpoint: "https://objects.example.com", Bucket: "plugins", Key: "plugin.zip", VersionID: "version-1",
				}}}},
			}
			result, err := c.CompileAgent(t.Context(), inlineAgent(harness, template))
			require.NoError(t, err)
			require.Equal(t, want, result.GoldenEgressDestinations)
			require.Subset(t, result.EgressDestinations, want, "sessions keep access to skill sources")

			template.Spec.Skills, template.Spec.Plugins = template.Spec.Skills[2:], nil
			result, err = c.CompileAgent(t.Context(), inlineAgent(harness, template))
			require.NoError(t, err)
			require.Empty(t, result.GoldenEgressDestinations, "OCI sources are mounted, not fetched")
		})
	}
}

func TestCompileSkillSourceNormalization(t *testing.T) {
	const digest = "@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var directory string
	for _, ref := range []string{"acme/skills" + digest, "docker.io/acme/skills" + digest, "index.docker.io/acme/skills" + digest} {
		result, err := translator.CompileSkillResources(&translator.TemplateConfiguration{Spec: v1alpha3.AgentTemplateSpec{
			Skills: []v1alpha3.AgentTemplateSkill{{Name: "skill", Source: v1alpha3.ArtifactSource{OCI: ref}}},
		}})
		require.NoError(t, err)
		if directory == "" {
			directory = result.Resources.Skills[0].Source.Directory
		}
		require.Equal(t, directory, result.Resources.Skills[0].Source.Directory)
	}
	for _, ref := range []string{"acme/skills:latest", "acme/skills@sha256:bad", "example.com/" + strings.Repeat("a", 450) + digest} {
		_, err := translator.CompileSkillResources(&translator.TemplateConfiguration{Spec: v1alpha3.AgentTemplateSpec{
			Skills: []v1alpha3.AgentTemplateSkill{{Name: "skill", Source: v1alpha3.ArtifactSource{OCI: ref}}},
		}})
		require.Error(t, err, "reference %q", ref)
	}
}

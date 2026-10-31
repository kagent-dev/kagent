package translator_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestCompileSharedSkillResources(t *testing.T) {
	source := v1alpha3.ArtifactSource{OCI: "registry.example.com/skills@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Path: "review"}
	other := source
	other.Path = "other"
	for _, tt := range []struct {
		name        string
		root, child []v1alpha3.AgentTemplateSkill
		plugins     []v1alpha3.PluginBundle
		wantNames   []string
		wantErr     string
	}{
		{name: "child only", child: []v1alpha3.AgentTemplateSkill{{Name: "review", Source: source}}, wantNames: []string{"review"}},
		{name: "identical selection", root: []v1alpha3.AgentTemplateSkill{{Name: "review", Source: source}}, child: []v1alpha3.AgentTemplateSkill{{Name: "review", Source: source}}, wantNames: []string{"review"}},
		{name: "different skills", root: []v1alpha3.AgentTemplateSkill{{Name: "root", Source: source}}, child: []v1alpha3.AgentTemplateSkill{{Name: "child", Source: other}}, wantNames: []string{"root", "child"}},
		{name: "conflicting sources", root: []v1alpha3.AgentTemplateSkill{{Name: "review", Source: source}}, child: []v1alpha3.AgentTemplateSkill{{Name: "review", Source: other}}, wantErr: "conflicting sources"},
		{name: "root plugin collision", plugins: []v1alpha3.PluginBundle{{Source: source, Skills: []string{"review"}}}, child: []v1alpha3.AgentTemplateSkill{{Name: "review", Source: source}}, wantErr: "root plugin skill"},
		{name: "child duplicates", child: []v1alpha3.AgentTemplateSkill{{Name: "review", Source: source}, {Name: "review", Source: source}}, wantErr: "duplicate skill"},
		{name: "root duplicates", root: []v1alpha3.AgentTemplateSkill{{Name: "review", Source: source}, {Name: "review", Source: source}}, wantErr: "duplicate skill"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := &translator.AgentInput{
				Template: &translator.TemplateConfiguration{Spec: v1alpha3.AgentTemplateSpec{Skills: tt.root, Plugins: tt.plugins}},
				Shared: []translator.AgentInputBinding{{Name: "child", Agent: &translator.AgentInput{
					Template: &translator.TemplateConfiguration{Spec: v1alpha3.AgentTemplateSpec{Skills: tt.child}},
				}}},
			}
			result, err := translator.CompileSharedSkillResources(root)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			var names []string
			for _, skill := range result.Resources.Skills {
				names = append(names, skill.Name)
				require.Equal(t, translator.SkillImage(source.OCI).MountPath(), skill.Source.Directory)
			}
			require.Equal(t, tt.wantNames, names)
			require.Equal(t, tt.root, root.Template.Spec.Skills, "compilation mutated root")
		})
	}
}

package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/pkg/adk"
)

func TestExecuteSystemMessageTemplate(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		lookup  map[string]string
		ctx     PromptTemplateContext
		want    string
		wantErr bool
		errMsg  string
	}{
		{
			name:   "include single key from source",
			raw:    `{{include "prompts/greeting"}} Have a great day.`,
			lookup: map[string]string{"prompts/greeting": "Hello, world!"},
			ctx:    PromptTemplateContext{},
			want:   "Hello, world! Have a great day.",
		},
		{
			name: "include multiple keys from same source",
			raw: `{{include "prompts/header"}}

You are a custom agent.

{{include "prompts/footer"}}`,
			lookup: map[string]string{
				"prompts/header": "# System Instructions",
				"prompts/footer": "# End of Instructions",
			},
			ctx: PromptTemplateContext{},
			want: `# System Instructions

You are a custom agent.

# End of Instructions`,
		},
		{
			name: "include keys from multiple sources",
			raw:  `{{include "builtin/safety"}} {{include "custom/rules"}}`,
			lookup: map[string]string{
				"builtin/safety": "Be safe.",
				"custom/rules":   "Follow the rules.",
			},
			ctx:  PromptTemplateContext{},
			want: "Be safe. Follow the rules.",
		},
		{
			name:   "variable interpolation - AgentName",
			raw:    `You are {{.AgentName}}, operating in {{.AgentNamespace}}.`,
			lookup: map[string]string{},
			ctx: PromptTemplateContext{
				AgentName:      "my-agent",
				AgentNamespace: "production",
			},
			want: "You are my-agent, operating in production.",
		},
		{
			name:   "variable interpolation - Description",
			raw:    `Agent purpose: {{.Description}}`,
			lookup: map[string]string{},
			ctx: PromptTemplateContext{
				Description: "A Kubernetes troubleshooting agent",
			},
			want: "Agent purpose: A Kubernetes troubleshooting agent",
		},
		{
			name:   "variable interpolation - ToolNames with range",
			raw:    `Tools: {{range .ToolNames}}{{.}}, {{end}}`,
			lookup: map[string]string{},
			ctx: PromptTemplateContext{
				ToolNames: []string{"get-pods", "describe-pod", "get-logs"},
			},
			want: "Tools: get-pods, describe-pod, get-logs, ",
		},
		{
			name:   "variable interpolation - SkillNames",
			raw:    `Skills: {{range .SkillNames}}{{.}}, {{end}}`,
			lookup: map[string]string{},
			ctx: PromptTemplateContext{
				SkillNames: []string{"k8s-debug", "helm-deploy"},
			},
			want: "Skills: k8s-debug, helm-deploy, ",
		},
		{
			name: "combined include and interpolation",
			raw: `{{include "builtin/skills-usage"}}

You are {{.AgentName}} ({{.Description}}).
Available tools: {{range .ToolNames}}{{.}}, {{end}}`,
			lookup: map[string]string{
				"builtin/skills-usage": "## Skills\nUse skills from /skills directory.",
			},
			ctx: PromptTemplateContext{
				AgentName:   "k8s-agent",
				Description: "Kubernetes helper",
				ToolNames:   []string{"get-pods", "apply-manifest"},
			},
			want: `## Skills
Use skills from /skills directory.

You are k8s-agent (Kubernetes helper).
Available tools: get-pods, apply-manifest, `,
		},
		{
			name:    "missing key in source",
			raw:     `{{include "prompts/nonexistent"}}`,
			lookup:  map[string]string{"prompts/other": "content"},
			ctx:     PromptTemplateContext{},
			wantErr: true,
			errMsg:  "nonexistent",
		},
		{
			name:    "invalid template syntax",
			raw:     `{{invalid syntax`,
			lookup:  map[string]string{},
			ctx:     PromptTemplateContext{},
			wantErr: true,
			errMsg:  "failed to parse",
		},
		{
			name:   "no nested template execution in included content",
			raw:    `{{include "prompts/tpl"}}`,
			lookup: map[string]string{"prompts/tpl": `This has {{.AgentName}} but should be literal`},
			ctx: PromptTemplateContext{
				AgentName: "should-not-appear",
			},
			// Included content is plain text, so {{.AgentName}} remains literal.
			want: `This has {{.AgentName}} but should be literal`,
		},
		{
			name:   "empty lookup map with no directives",
			raw:    `Plain system message with no templates.`,
			lookup: map[string]string{},
			ctx:    PromptTemplateContext{},
			want:   "Plain system message with no templates.",
		},
		{
			name:   "empty ToolNames and SkillNames",
			raw:    `Agent: {{.AgentName}}, tools: {{len .ToolNames}}, skills: {{len .SkillNames}}`,
			lookup: map[string]string{},
			ctx: PromptTemplateContext{
				AgentName: "test",
			},
			want: "Agent: test, tools: 0, skills: 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := executeSystemMessageTemplate(tt.raw, tt.lookup, tt.ctx)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildTemplateContext(t *testing.T) {
	tests := []struct {
		name    string
		agent   *v1alpha2.Agent
		cfg     *adk.AgentConfig
		wantCtx PromptTemplateContext
	}{
		{
			name: "tool names from config, skill names from spec",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-agent",
					Namespace: "production",
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "A helpful agent",
					Declarative: &v1alpha2.DeclarativeAgentSpec{},
					Skills: &v1alpha2.SkillForAgent{
						Refs: []string{"ghcr.io/org/skill-k8s:v1", "ghcr.io/org/skill-helm"},
						GitRefs: []v1alpha2.GitRepo{
							{URL: "https://github.com/org/my-skills.git", Name: "custom-skills"},
							{URL: "https://github.com/org/other-repo.git"},
						},
					},
				},
			},
			cfg: &adk.AgentConfig{
				HttpTools: []adk.HttpMcpServerConfig{
					{Tools: []string{"get-pods", "describe-pod"}},
					{Tools: []string{"helm-install"}},
				},
			},
			wantCtx: PromptTemplateContext{
				AgentName:      "my-agent",
				AgentNamespace: "production",
				Description:    "A helpful agent",
				ToolNames:      []string{"get-pods", "describe-pod", "helm-install"},
				SkillNames:     []string{"skill-k8s", "skill-helm", "custom-skills", "other-repo"},
			},
		},
		{
			name: "skills with OCI digests and git URLs with query/fragment",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-agent",
					Namespace: "production",
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "A helpful agent",
					Declarative: &v1alpha2.DeclarativeAgentSpec{},
					Skills: &v1alpha2.SkillForAgent{
						Refs: []string{
							"ghcr.io/org/skill-k8s@sha256:abcdef0123456789",
							"ghcr.io/org/skill-helm:v1@sha256:0123456789abcdef",
						},
						GitRefs: []v1alpha2.GitRepo{
							{
								URL:  "https://github.com/org/my-skills.git?ref=main#subdir",
								Name: "custom-skills",
							},
							{
								URL: "https://github.com/org/other-repo.git?ref=dev#path/to/skills",
							},
						},
					},
				},
			},
			cfg: &adk.AgentConfig{
				HttpTools: []adk.HttpMcpServerConfig{
					{Tools: []string{"get-pods", "describe-pod"}},
					{Tools: []string{"helm-install"}},
				},
			},
			wantCtx: PromptTemplateContext{
				AgentName:      "my-agent",
				AgentNamespace: "production",
				Description:    "A helpful agent",
				ToolNames:      []string{"get-pods", "describe-pod", "helm-install"},
				SkillNames:     []string{"skill-k8s", "skill-helm", "custom-skills", "other-repo"},
			},
		},
		{
			name: "SSE tools included",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sse-agent",
					Namespace: "default",
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "SSE agent",
					Declarative: &v1alpha2.DeclarativeAgentSpec{},
				},
			},
			cfg: &adk.AgentConfig{
				SseTools: []adk.SseMcpServerConfig{
					{Tools: []string{"sse-tool-1", "sse-tool-2"}},
				},
			},
			wantCtx: PromptTemplateContext{
				AgentName:      "sse-agent",
				AgentNamespace: "default",
				Description:    "SSE agent",
				ToolNames:      []string{"sse-tool-1", "sse-tool-2"},
				SkillNames:     nil,
			},
		},
		{
			name: "empty config",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-agent",
					Namespace: "default",
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "Simple",
					Declarative: &v1alpha2.DeclarativeAgentSpec{},
				},
			},
			cfg: &adk.AgentConfig{},
			wantCtx: PromptTemplateContext{
				AgentName:      "simple-agent",
				AgentNamespace: "default",
				Description:    "Simple",
				ToolNames:      nil,
				SkillNames:     nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTemplateContext(tt.agent, tt.cfg)
			assert.Equal(t, tt.wantCtx, got)
		})
	}
}

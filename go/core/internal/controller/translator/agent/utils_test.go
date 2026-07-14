package agent_test

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
)

func TestGetA2AAgentCard(t *testing.T) {
	tests := []struct {
		name                 string
		agent                *v1alpha2.Agent
		wantName             string
		wantDescription      string
		wantURL              string
		wantSkills           []a2atype.AgentSkill
		wantIconURL          string
		wantDocumentationURL string
		wantVersion          string
		wantProvider         *a2atype.AgentProvider
	}{
		{
			name: "declarative agent with a2a config and skills",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "default",
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Description: "A test agent",
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						A2AConfig: &v1alpha2.A2AConfig{
							Skills: []v1alpha2.AgentSkill{
								{Name: "skill-1"},
								{Name: "skill-2"},
							},
						},
					},
				},
			},
			wantName:        "test_agent",
			wantDescription: "A test agent",
			wantURL:         "http://test-agent.default:8080",
			wantSkills:      []a2atype.AgentSkill{{Name: "skill-1"}, {Name: "skill-2"}},
		},
		{
			name: "declarative agent with nil declarative spec",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nil-declarative",
					Namespace: "default",
				},
				Spec: v1alpha2.AgentSpec{
					Type:        v1alpha2.AgentType_Declarative,
					Declarative: nil,
				},
			},
			wantName:        "nil_declarative",
			wantDescription: "",
			wantURL:         "http://nil-declarative.default:8080",
			wantSkills:      []a2atype.AgentSkill{},
		},
		{
			name: "declarative agent with nil a2a config",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-a2a",
					Namespace: "default",
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						A2AConfig: nil,
					},
				},
			},
			wantName:        "no_a2a",
			wantDescription: "",
			wantURL:         "http://no-a2a.default:8080",
			wantSkills:      []a2atype.AgentSkill{},
		},
		{
			name: "BYO agent",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "byo-agent",
					Namespace: "default",
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_BYO,
				},
			},
			wantName:        "byo_agent",
			wantDescription: "",
			wantURL:         "http://byo-agent.default:8080",
			wantSkills:      []a2atype.AgentSkill{},
		},
		{
			name: "agent with card metadata",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "meta-agent",
					Namespace: "default",
				},
				Spec: v1alpha2.AgentSpec{
					Type:             v1alpha2.AgentType_BYO,
					IconURL:          "https://example.com/icon.png",
					DocumentationURL: "https://example.com/docs",
					Version:          "1.2.3",
					Provider: &v1alpha2.AgentProvider{
						Organization: "Acme",
						URL:          "https://acme.example.com",
					},
				},
			},
			wantName:             "meta_agent",
			wantURL:              "http://meta-agent.default:8080",
			wantSkills:           []a2atype.AgentSkill{},
			wantIconURL:          "https://example.com/icon.png",
			wantDocumentationURL: "https://example.com/docs",
			wantVersion:          "1.2.3",
			wantProvider:         &a2atype.AgentProvider{Org: "Acme", URL: "https://acme.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := translator.GetA2AAgentCard(tt.agent)

			assert.NotNil(t, card)
			assert.Equal(t, tt.wantName, card.Name)
			assert.Equal(t, tt.wantDescription, card.Description)
			require.Len(t, card.SupportedInterfaces, 2)
			assert.Equal(t, tt.wantURL, card.SupportedInterfaces[0].URL)
			assert.Equal(t, a2atype.TransportProtocolJSONRPC, card.SupportedInterfaces[0].ProtocolBinding)
			assert.Equal(t, a2atype.ProtocolVersion("0.3"), card.SupportedInterfaces[0].ProtocolVersion)
			assert.Equal(t, tt.wantURL, card.SupportedInterfaces[1].URL)
			assert.Equal(t, a2atype.TransportProtocolJSONRPC, card.SupportedInterfaces[1].ProtocolBinding)
			assert.Equal(t, a2atype.Version, card.SupportedInterfaces[1].ProtocolVersion)
			assert.Equal(t, tt.wantSkills, card.Skills)
			assert.Equal(t, []string{"text"}, card.DefaultInputModes)
			assert.Equal(t, []string{"text"}, card.DefaultOutputModes)
			assert.True(t, card.Capabilities.Streaming)
			assert.False(t, card.Capabilities.PushNotifications)
			assert.Equal(t, tt.wantIconURL, card.IconURL)
			assert.Equal(t, tt.wantDocumentationURL, card.DocumentationURL)
			assert.Equal(t, tt.wantVersion, card.Version)
			assert.Equal(t, tt.wantProvider, card.Provider)
		})
	}
}

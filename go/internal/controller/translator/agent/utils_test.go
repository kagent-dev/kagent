package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	translator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
)

func TestGetA2AAgentCard(t *testing.T) {
	tests := []struct {
		name           string
		agent          *v1alpha2.Agent
		wantName       string
		wantSkillCount int
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
			wantName:       "test_agent",
			wantSkillCount: 2,
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
			wantName:       "nil_declarative",
			wantSkillCount: 0,
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
			wantName:       "no_a2a",
			wantSkillCount: 0,
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
			wantName:       "byo_agent",
			wantSkillCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := translator.GetA2AAgentCard(tt.agent)

			assert.NotNil(t, card)
			assert.Equal(t, tt.wantName, card.Name)
			assert.Len(t, card.Skills, tt.wantSkillCount)
		})
	}
}

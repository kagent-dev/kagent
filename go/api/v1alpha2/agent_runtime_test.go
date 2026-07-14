package v1alpha2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEffectiveDeclarativeRuntime(t *testing.T) {
	tests := []struct {
		name string
		spec *AgentSpec
		want DeclarativeRuntime
	}{
		{
			name: "nil spec defaults to Python",
			spec: nil,
			want: DeclarativeRuntime_Python,
		},
		{
			name: "unset runtime defaults to Python",
			spec: &AgentSpec{Type: AgentType_Declarative, Declarative: &DeclarativeAgentSpec{}},
			want: DeclarativeRuntime_Python,
		},
		{
			name: "explicit Python runtime",
			spec: &AgentSpec{Type: AgentType_Declarative, Declarative: &DeclarativeAgentSpec{Runtime: DeclarativeRuntime_Python}},
			want: DeclarativeRuntime_Python,
		},
		{
			name: "explicit Go runtime is honored",
			spec: &AgentSpec{Type: AgentType_Declarative, Declarative: &DeclarativeAgentSpec{Runtime: DeclarativeRuntime_Go}},
			want: DeclarativeRuntime_Go,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, EffectiveDeclarativeRuntime(tt.spec))
		})
	}
}

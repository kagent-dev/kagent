package reconciler

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/assert"
)

func TestValidateRuntimeFeatures(t *testing.T) {
	compaction := &v1alpha2.ContextConfig{
		Compaction: &v1alpha2.ContextCompressionConfig{CompactionInterval: new(5), OverlapSize: new(2)},
	}
	tests := []struct {
		name        string
		declarative *v1alpha2.DeclarativeAgentSpec
		wantWarning string
	}{
		{
			name:        "go runtime honours context compaction",
			declarative: &v1alpha2.DeclarativeAgentSpec{Runtime: v1alpha2.DeclarativeRuntime_Go, Context: compaction},
		},
		{
			name:        "default runtime honours context compaction",
			declarative: &v1alpha2.DeclarativeAgentSpec{Context: compaction},
		},
		{
			name:        "python runtime honours context compaction",
			declarative: &v1alpha2.DeclarativeAgentSpec{Runtime: v1alpha2.DeclarativeRuntime_Python, Context: compaction},
		},
		{
			name:        "go runtime still reports code execution",
			declarative: &v1alpha2.DeclarativeAgentSpec{Runtime: v1alpha2.DeclarativeRuntime_Go, ExecuteCodeBlocks: new(true), Context: compaction},
			wantWarning: "code execution",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &v1alpha2.Agent{Spec: v1alpha2.AgentSpec{Type: v1alpha2.AgentType_Declarative, Declarative: tt.declarative}}
			warning := (&kagentReconciler{}).validateRuntimeFeatures(agent)
			if tt.wantWarning == "" {
				assert.Empty(t, warning)
				return
			}
			assert.Contains(t, warning, tt.wantWarning)
			assert.NotContains(t, warning, "compaction")
		})
	}
}

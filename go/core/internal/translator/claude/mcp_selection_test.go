package claude

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestLocalAgentToolExclusions(t *testing.T) {
	for _, tt := range []struct {
		name         string
		selected     []string
		observed     int64
		discovered   []*v1alpha3.MCPTool
		wantExcluded []string
		wantWarning  bool
		wantErr      string
	}{
		{name: "whole server without discovery"},
		{name: "current discovery", selected: []string{"read"}, observed: 2, discovered: []*v1alpha3.MCPTool{{Name: "read"}, {Name: "write"}}, wantExcluded: []string{"mcp__tools__write"}},
		{name: "stale discovery", selected: []string{"read"}, observed: 1, discovered: []*v1alpha3.MCPTool{{Name: "read"}, {Name: "write"}}, wantWarning: true},
		{name: "no discovery", selected: []string{"read"}, wantWarning: true},
		{name: "empty current discovery", selected: []string{"read"}, observed: 2, wantWarning: true},
		{name: "invalid discovered tool", selected: []string{"read"}, observed: 2, discovered: []*v1alpha3.MCPTool{nil}, wantWarning: true},
		{name: "unknown selected tool", selected: []string{"missing"}, observed: 2, discovered: []*v1alpha3.MCPTool{{Name: "read"}}, wantErr: "selects unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := &v1alpha3.RemoteMCPServer{
				ObjectMeta: metav1.ObjectMeta{Name: "tools", Namespace: "test", Generation: 2},
				Status:     v1alpha3.RemoteMCPServerStatus{ObservedGeneration: tt.observed, DiscoveredTools: tt.discovered},
			}
			excluded, warnings, err := localAgentToolExclusions("test", []translator.ResolvedMCPTool{{Binding: v1alpha3.MCPToolBinding{Tools: tt.selected}, Server: server}})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantExcluded, excluded)
			if tt.wantWarning {
				require.Equal(t, []string{mcpSelectionWarning(tt.selected, server)}, warnings, "use the same discovery fallback warning as the root")
			} else {
				require.Empty(t, warnings)
			}
		})
	}
}

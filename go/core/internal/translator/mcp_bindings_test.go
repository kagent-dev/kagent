package translator

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestMCPBindingNamesRejectConflictingApprovalScopes(t *testing.T) {
	server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "tools", Namespace: "test"}}
	for _, selected := range [][]string{nil, {"execute"}} {
		bindings := []ResolvedMCPTool{
			{Server: server, Binding: v1alpha3.MCPToolBinding{Tools: selected}},
			{Server: server, Binding: v1alpha3.MCPToolBinding{Tools: []string{"execute"}, RequireApproval: true}},
		}
		if _, err := MCPBindingNames(server.Namespace, bindings); err == nil {
			t.Errorf("MCPBindingNames(%v) accepted conflicting approval scopes", selected)
		}
	}
}

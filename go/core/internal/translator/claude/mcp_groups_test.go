package claude

import (
	"context"
	"reflect"
	"testing"

	"istio.io/istio/pkg/kube/krt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestCompileMCPSeparatesApprovalGroups(t *testing.T) {
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "tools", Namespace: "test", Generation: 1},
		Spec:       v1alpha3.RemoteMCPServerSpec{URL: "https://mcp.example.com/mcp"},
		Status: v1alpha3.RemoteMCPServerStatus{ObservedGeneration: 1,
			DiscoveredTools: []*v1alpha3.MCPTool{{Name: "edit"}, {Name: "execute"}}},
	}
	bindings := []translator.ResolvedMCPTool{
		{Server: server, Binding: v1alpha3.MCPToolBinding{Tools: []string{"edit"}}},
		{Server: server, Binding: v1alpha3.MCPToolBinding{Tools: []string{"execute"}, RequireApproval: true}},
	}
	compiler := NewCompiler(krt.TestingDummyContext{}, translator.Collections{}, testRuntimeDefaults())
	for range 2 {
		compiled, err := compiler.compileMCP(context.Background(), "test", bindings)
		if err != nil {
			t.Fatal(err)
		}
		direct, reviewed := compiled.servers["tools"], compiled.servers["tools-approval"]
		if len(compiled.servers) != 2 || direct.RequireApproval || !reviewed.RequireApproval ||
			!reflect.DeepEqual(direct.Tools, []string{"edit"}) || !reflect.DeepEqual(reviewed.Tools, []string{"execute"}) {
			t.Fatalf("compiled approval groups = %#v", compiled.servers)
		}
		wantDenied := []string{"mcp__tools-approval__edit", "mcp__tools__execute"}
		if !reflect.DeepEqual(compiled.disallowedTools, wantDenied) {
			t.Fatalf("disallowed tools = %v, want %v", compiled.disallowedTools, wantDenied)
		}
		bindings[0], bindings[1] = bindings[1], bindings[0]
	}
}

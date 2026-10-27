package codex

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
		ObjectMeta: metav1.ObjectMeta{Name: "tools", Namespace: "test"},
		Spec:       v1alpha3.RemoteMCPServerSpec{URL: "https://mcp.example.com/mcp"},
	}
	bindings := []translator.ResolvedMCPTool{
		{Server: server, Binding: v1alpha3.MCPToolBinding{Tools: []string{"edit"}}},
		{Server: server, Binding: v1alpha3.MCPToolBinding{Tools: []string{"execute"}, RequireApproval: true}},
	}
	compiler := NewCompiler(krt.TestingDummyContext{}, translator.Collections{}, testRuntimeDefaults())
	compiled, err := compiler.compileMCP(context.Background(), "test", bindings)
	if err != nil {
		t.Fatal(err)
	}
	direct, reviewed := compiled.servers["tools"], compiled.servers["tools-approval"]
	if len(compiled.servers) != 2 || direct.RequireApproval || !reviewed.RequireApproval ||
		!reflect.DeepEqual(direct.EnabledTools, []string{"edit"}) || !reflect.DeepEqual(reviewed.EnabledTools, []string{"execute"}) {
		t.Fatalf("compiled approval groups = %#v", compiled.servers)
	}
}

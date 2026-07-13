package substrate

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSessionDBURL covers the runtime dialect of the durable-dir session-store URL the
// translator bakes into the rendered config (AgentConfig.session_db_url): python's SQLAlchemy
// async engine needs the aiosqlite driver segment; the Go store accepts either form, and BYO
// gets the python form.
func TestSessionDBURL(t *testing.T) {
	t.Parallel()

	b := &AgentsBackend{}
	cmd := "/serve"
	for _, tc := range []struct {
		name string
		spec v1alpha2.AgentSpec
		want string
	}{
		{
			name: "python",
			spec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{Runtime: v1alpha2.DeclarativeRuntime_Python},
			},
			want: "sqlite+aiosqlite:////data/sessions.db",
		},
		{
			name: "go",
			spec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{Runtime: v1alpha2.DeclarativeRuntime_Go},
			},
			want: "sqlite:////data/sessions.db",
		},
		{
			name: "byo",
			spec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_BYO,
				BYO:  &v1alpha2.BYOAgentSpec{Deployment: &v1alpha2.ByoDeploymentSpec{Image: "example/agent:latest", Cmd: &cmd}},
			},
			want: "sqlite+aiosqlite:////data/sessions.db",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sa := &v1alpha2.SandboxAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "my-agent", Namespace: "kagent"},
				Spec:       v1alpha2.SandboxAgentSpec{AgentSpec: tc.spec},
			}
			require.Equal(t, tc.want, b.SessionDBURL(sa))
		})
	}
}

package substrate

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSessionDBURL covers the runtime dialect of the durable-dir session-store URL the
// translator bakes into the rendered config (AgentConfig.session_db_url): Python BYO agents may
// need the aiosqlite driver segment, while the declarative Go runtime uses the native form.
func TestSessionDBURL(t *testing.T) {
	t.Parallel()

	b := &AgentsBackend{}
	cmd := "/serve"
	for _, tc := range []struct {
		name string
		spec v1alpha3.AgentSpec
		want string
	}{
		{
			name: "declarative",
			spec: v1alpha3.AgentSpec{
				Type:        v1alpha3.AgentType_Declarative,
				Declarative: &v1alpha3.DeclarativeAgentSpec{},
			},
			want: "sqlite:////data/sessions.db",
		},
		{
			name: "byo",
			spec: v1alpha3.AgentSpec{
				Type: v1alpha3.AgentType_BYO,
				BYO:  &v1alpha3.BYOAgentSpec{Image: "example/agent:latest", Cmd: &cmd},
			},
			want: "sqlite+aiosqlite:////data/sessions.db",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sa := &v1alpha3.SandboxAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "my-agent", Namespace: "kagent"},
				Spec:       tc.spec,
			}
			require.Equal(t, tc.want, b.SessionDBURL(sa))
		})
	}
}

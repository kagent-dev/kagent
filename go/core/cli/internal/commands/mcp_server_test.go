package commands

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
)

func TestListMCPServers(t *testing.T) {
	t.Run("Given servers in team-a and team-ab, when listed in team-a, assert that only team-a's servers appear by name with their tools", func(t *testing.T) {
		response := &apiv1.ListToolServersResponse{ToolServers: []*apiv1.ToolServer{
			{Ref: "team-a/github", GroupKind: "RemoteMCPServer.api.kagent.dev", DiscoveredTools: []*apiv1.DiscoveredTool{{Name: "create_issue"}, {Name: "list_issues"}}},
			{Ref: "team-ab/jira", GroupKind: "RemoteMCPServer.api.kagent.dev", DiscoveredTools: []*apiv1.DiscoveredTool{{Name: "create_ticket"}}},
		}}
		var output bytes.Buffer

		require.NoError(t, writeMCPServers(&output, clioutput.FormatTable, "team-a", response))
		assert.Regexp(t, `\| github\s+\|\s+RemoteMCPServer.api.kagent.dev\s+\|\s+create_issue`, output.String())
		assert.Contains(t, output.String(), "list_issues")
		assert.NotContains(t, output.String(), "jira")
		assert.NotContains(t, output.String(), "create_ticket")
	})
}

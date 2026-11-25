package commands

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
)

func TestListHarnesses(t *testing.T) {
	t.Run("Given Harnesses for two runtimes, when listed as a table, assert that each row shows its name and runtime", func(t *testing.T) {
		response := &apiv1.ListHarnessesResponse{Harnesses: []*apiv1.Harness{
			{Ref: &apiv1.ResourceReference{Namespace: "kagent", Name: "kagent-default"}, Runtime: "kagent"},
			{Ref: &apiv1.ResourceReference{Namespace: "kagent", Name: "codex-default"}, Runtime: "codex"},
		}}
		var output bytes.Buffer

		require.NoError(t, writeHarnesses(&output, clioutput.FormatTable, response))
		assert.Regexp(t, `kagent-default\s+\S\s+kagent\s`, output.String())
		assert.Regexp(t, `codex-default\s+\S\s+codex\s`, output.String())
	})
}

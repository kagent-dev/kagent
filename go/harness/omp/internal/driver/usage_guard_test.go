package driver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOmpTurnCarriesItsUsageOnTheOutcome guards the turn usage the platform
// reads from an omp turn's final task status: the session's spend between two
// snapshots becomes the runtime's Outcome usage, cost included.
func TestOmpTurnCarriesItsUsageOnTheOutcome(t *testing.T) {
	var before, after sessionStats
	after.AssistantMessages, after.Cost = 2, 0.25
	after.Tokens.Input, after.Tokens.Output = 120, 40
	usage := turnUsage(before, after)
	require.NotNil(t, usage)
	require.NotNil(t, usage.TotalCostUSD)
	require.InDelta(t, 0.25, *usage.TotalCostUSD, 1e-9)
	require.Equal(t, 2, usage.NumTurns)
	require.Equal(t, 120, usage.InputTokens)
	require.Equal(t, 40, usage.OutputTokens)
}

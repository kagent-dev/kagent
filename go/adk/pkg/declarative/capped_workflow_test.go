package declarative

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
)

// peakFanOut runs four parallel branches that each hold a slot for a while and
// returns the largest number of branches that ran at once.
func peakFanOut(t *testing.T, maxConcurrency string) int64 {
	t.Helper()
	var active, peak atomic.Int64
	reg := NewRegistry()
	require.NoError(t, reg.RegisterFunction("work", func(agent.Context, any) (Result, error) {
		n := active.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		time.Sleep(50 * time.Millisecond)
		active.Add(-1)
		return Output("done"), nil
	}))
	require.NoError(t, reg.RegisterFunction("gathered", constant("gathered")))
	spec, err := reg.Parse([]byte(docHeader + `  - name: fan
    agent_class: Workflow
` + maxConcurrency + `    nodes:
      - { id: a, type: function, function: work }
      - { id: b, type: function, function: work }
      - { id: c, type: function, function: work }
      - { id: d, type: function, function: work }
      - { id: gather, type: join }
      - { id: done, type: function, function: gathered }
    edges:
      - { from: START, to: a }
      - { from: a, to: gather }
      - { from: START, to: b }
      - { from: b, to: gather }
      - { from: START, to: c }
      - { from: c, to: gather }
      - { from: START, to: d }
      - { from: d, to: gather }
      - { from: gather, to: done }
`))
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	require.NoError(t, err)
	svc, id := newSession(t)
	got := outputs(run(t, sys.Root(), userMessage("go"), svc, id))
	require.Equal(t, "gathered", got[len(got)-1])
	return peak.Load()
}

func TestWorkflowMaxConcurrencyCapsFanOut(t *testing.T) {
	require.Equal(t, int64(2), peakFanOut(t, "    max_concurrency: 2\n"))
	require.Equal(t, int64(1), peakFanOut(t, "    max_concurrency: 1\n"))
	require.Greater(t, peakFanOut(t, ""), int64(2), "without a cap every branch starts at once")
}

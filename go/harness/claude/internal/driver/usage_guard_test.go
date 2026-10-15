package driver

import (
	"strings"
	"testing"
)

// TestClaudeTurnCarriesItsUsageOnTheOutcome guards the turn usage the platform
// reads from a claude turn's final task status: a completed and a failed turn
// both hand the usage Claude Code reported to the runtime's Outcome.
func TestClaudeTurnCarriesItsUsageOnTheOutcome(t *testing.T) {
	for _, result := range []string{
		`{"type":"result","subtype":"success","result":"done","total_cost_usd":0.25,"num_turns":3,"usage":{"input_tokens":120,"cache_read_input_tokens":3000,"cache_creation_input_tokens":500,"output_tokens":40},"session_id":"22222222-2222-4222-8222-222222222222"}`,
		`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"boom","total_cost_usd":0.25,"num_turns":3,"usage":{"input_tokens":120,"cache_read_input_tokens":3000,"cache_creation_input_tokens":500,"output_tokens":40},"session_id":"22222222-2222-4222-8222-222222222222"}`,
	} {
		var last Event
		if err := ParseJSONL(strings.NewReader(result+"\n"), 4096, func(event Event) error { last = event; return nil }); err != nil {
			t.Fatal(err)
		}
		outcome, err := emitEvent(last, &recordingSink{}, false)
		if err != nil {
			t.Fatal(err)
		}
		usage := outcome.Usage
		if usage == nil || usage.TotalCostUSD == nil || *usage.TotalCostUSD != 0.25 || usage.NumTurns != 3 ||
			usage.InputTokens != 3620 || usage.CachedTokens != 3000 || usage.OutputTokens != 40 {
			t.Fatalf("outcome usage = %#v, want the turn's reported usage", usage)
		}
	}
}

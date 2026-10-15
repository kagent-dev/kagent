package driver

import "testing"

// TestCodexTurnCarriesItsUsageOnTheOutcome guards the turn usage the platform
// reads from a codex turn's final task status: the turn's token counts reach
// the runtime's Outcome.
func TestCodexTurnCarriesItsUsageOnTheOutcome(t *testing.T) {
	outcome := translateAll(t, newEventTranslator("thread", "turn"), []string{
		`{"jsonrpc":"2.0","method":"thread/tokenUsage/updated","params":{"threadId":"thread","turnId":"turn","tokenUsage":{"total":{"inputTokens":120,"cachedInputTokens":20,"outputTokens":40},"last":{"inputTokens":120,"cachedInputTokens":20,"outputTokens":40}}}}`,
		`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread","turn":{"id":"turn","status":"completed"}}}`,
	})
	usage := outcome.Usage
	if usage == nil || usage.InputTokens != 120 || usage.CachedTokens != 20 || usage.OutputTokens != 40 {
		t.Fatalf("outcome usage = %#v, want the turn's reported tokens", usage)
	}
}

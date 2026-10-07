package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
)

// The OTel GenAI conventions name the tool attributes. kagent's generated
// contract lacks them because the ADK records them on its own spans.
const (
	BaggageToolName   = "gen_ai.tool.name"
	BaggageToolCallID = "gen_ai.tool.call.id"
)

// WithBaggage adds string attributes to the W3C baggage of ctx, replacing
// members with the same key and keeping the others. Empty values are skipped.
// Baggage leaves the process only when OTEL_PROPAGATORS includes baggage.
func WithBaggage(ctx context.Context, attributes ...attribute.KeyValue) context.Context {
	bag := baggage.FromContext(ctx)
	for _, attr := range attributes {
		value := attr.Value.AsString()
		if value == "" {
			continue
		}
		// Keys are kagent's attribute names, which are valid baggage keys, and
		// raw values are percent-encoded when injected; an error here can only
		// come from the W3C size limits, where dropping the member is correct.
		member, err := baggage.NewMemberRaw(string(attr.Key), value)
		if err != nil {
			continue
		}
		if next, err := bag.SetMember(member); err == nil {
			bag = next
		}
	}
	return baggage.ContextWithBaggage(ctx, bag)
}

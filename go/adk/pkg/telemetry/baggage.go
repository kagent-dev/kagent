package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
)

// WithBaggage adds string attributes to the W3C baggage of ctx, replacing
// members with the same key and keeping the others. Empty values are skipped.
// Baggage leaves the process only when OTEL_PROPAGATORS includes baggage.
func WithBaggage(ctx context.Context, attributes ...attribute.KeyValue) context.Context {
	bag := baggage.FromContext(ctx)
	for _, attr := range attributes {
		// Remove existing baggage entries for provided attributes
		bag.DeleteMember(string(attr.Key))

		member, err := baggage.NewMemberRaw(string(attr.Key), attr.Value.AsString())
		if err != nil {
			continue
		}
		if next, err := bag.SetMember(member); err == nil {
			bag = next
		}
	}
	return baggage.ContextWithBaggage(ctx, bag)
}

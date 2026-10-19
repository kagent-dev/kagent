package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
)

// WithBaggage adds string attributes to the W3C baggage of ctx, replacing
// members with the same key and keeping the others.
// Baggage leaves the process only when OTEL_PROPAGATORS includes baggage.
func WithBaggage(ctx context.Context, attributes ...attribute.KeyValue) context.Context {
	bag := baggage.FromContext(ctx)

	// otel baggage does not allow duplicates, baggage.Baggage is a map[string]Item
	// setting a member overwrites a previous member with the same key.
	for _, attr := range attributes {
		bag = bag.DeleteMember(string(attr.Key))

		if len(attr.Value.AsString()) == 0 {
			continue
		}

		member, err := baggage.NewMemberRaw(string(attr.Key), attr.Value.AsString())
		if err != nil {
			continue
		}

		// in the error case the original baggage is returned unmodified
		bag, _ = bag.SetMember(member)
	}

	if len(bag.String()) > 8192 {
		var err error
		bag, err = baggage.New()
		if err != nil {
			return baggage.ContextWithoutBaggage(ctx)
		}

		for _, attr := range attributes {
			member, err := baggage.NewMemberRaw(string(attr.Key), attr.Value.AsString())
			if err != nil {
				continue
			}

			bag, _ = bag.SetMember(member)
		}
	}

	return baggage.ContextWithBaggage(ctx, bag)
}

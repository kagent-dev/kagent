package telemetry

import (
	"cmp"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
)

func baggageEqual(a, b baggage.Baggage) bool {
	am := a.Members()
	bm := b.Members()

	slices.SortFunc(am, func(a, b baggage.Member) int {
		return cmp.Compare(a.Key(), b.Key())
	})
	slices.SortFunc(bm, func(a, b baggage.Member) int {
		return cmp.Compare(a.Key(), b.Key())
	})

	return slices.EqualFunc(am, bm, func(a, b baggage.Member) bool {
		return a.String() == b.String()
	})
}

func TestWithBaggage(t *testing.T) {
	parseBaggage := func(value string) baggage.Baggage {
		t.Helper()

		bag, err := baggage.Parse(value)
		if err != nil {
			t.Fatalf("parse baggage %q: %v", value, err)
		}
		return bag
	}

	cases := []struct {
		name     string
		input    baggage.Baggage
		attrs    []attribute.KeyValue
		expected baggage.Baggage
	}{
		{
			name:     "no attributes",
			input:    parseBaggage("agent.name=researcher,conversation.id=conversation-1"),
			expected: parseBaggage("agent.name=researcher,conversation.id=conversation-1"),
		},
		{
			name:  "add attributes",
			attrs: []attribute.KeyValue{attribute.String("agent.name", "researcher"), attribute.String("conversation.id", "conversation-1")},
			expected: parseBaggage(
				"agent.name=researcher,conversation.id=conversation-1",
			),
		},
		{
			name:     "preserve unrelated and replace matching member",
			input:    parseBaggage("agent.name=old,tenant.id=tenant-1"),
			attrs:    []attribute.KeyValue{attribute.String("agent.name", "new")},
			expected: parseBaggage("agent.name=new,tenant.id=tenant-1"),
		},
		{
			name:     "empty value removes matching member",
			input:    parseBaggage("agent.name=researcher,tenant.id=tenant-1"),
			attrs:    []attribute.KeyValue{attribute.String("agent.name", "")},
			expected: parseBaggage("tenant.id=tenant-1"),
		},
		{
			name:     "last duplicate attribute wins",
			input:    parseBaggage("agent.name=old"),
			attrs:    []attribute.KeyValue{attribute.String("agent.name", "first"), attribute.String("agent.name", "last")},
			expected: parseBaggage("agent.name=last"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := baggage.ContextWithBaggage(t.Context(), tc.input)

			ctx = WithBaggage(ctx, tc.attrs...)

			actual := baggage.FromContext(ctx)
			if !baggageEqual(actual, tc.expected) {
				t.Errorf("actual baggage doesn't not equal expected\n> expected %q\n> actual %q", tc.expected.String(), actual.String())
			}
		})
	}
}

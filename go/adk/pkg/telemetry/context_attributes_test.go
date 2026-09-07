package telemetry

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"go.opentelemetry.io/contrib/processors/baggagecopy"
	"go.opentelemetry.io/otel/baggage"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setAllowlist(t *testing.T, allowlist string) {
	t.Helper()
	t.Setenv(traceContextKeysEnvVar, allowlist)
	resetAllowedContextMappings()
	t.Cleanup(resetAllowedContextMappings)
}

func baggageContext(t *testing.T, members map[string]string) context.Context {
	t.Helper()

	built := make([]baggage.Member, 0, len(members))
	for key, value := range members {
		member, err := baggage.NewMember(key, value)
		if err != nil {
			t.Fatalf("baggage.NewMember(%q, %q): %v", key, value, err)
		}
		built = append(built, member)
	}
	bag, err := baggage.New(built...)
	if err != nil {
		t.Fatalf("baggage.New: %v", err)
	}
	return baggage.ContextWithBaggage(context.Background(), bag)
}

func baggageValues(ctx context.Context) map[string]string {
	out := map[string]string{}
	for _, member := range baggage.FromContext(ctx).Members() {
		out[member.Key()] = member.Value()
	}
	return out
}

func TestContextWithPromotedMetadata(t *testing.T) {
	tests := []struct {
		name        string
		allowlist   string
		baggageVals map[string]string
		metadata    map[string]any
		want        map[string]string
	}{
		{
			name:        "empty allowlist is a no-op",
			baggageVals: map[string]string{"user.id": "opaque-subject"},
			metadata:    map[string]any{"user.id": "from-metadata"},
			want:        map[string]string{"user.id": "opaque-subject"},
		},
		{
			name:        "leaves existing baggage in place",
			allowlist:   "user.id",
			baggageVals: map[string]string{"user.id": "from-baggage"},
			metadata:    map[string]any{"user.id": "from-metadata"},
			want:        map[string]string{"user.id": "from-baggage"},
		},
		{
			name:      "promotes allowlisted metadata into baggage",
			allowlist: "user.id,thread_id",
			metadata:  map[string]any{"user.id": "opaque-subject", "thread_id": "T123", "secret": "nope"},
			want:      map[string]string{"user.id": "opaque-subject", "thread_id": "T123"},
		},
		{
			name:        "empty metadata does not wipe baggage",
			allowlist:   "user.id",
			baggageVals: map[string]string{"user.id": "from-baggage"},
			metadata:    map[string]any{"user.id": "  "},
			want:        map[string]string{"user.id": "from-baggage"},
		},
		{
			name:        "remaps baggage from onto to when to is absent",
			allowlist:   `[{"from":"sub","to":"user.id"}]`,
			baggageVals: map[string]string{"sub": "opaque-subject"},
			want:        map[string]string{"sub": "opaque-subject", "user.id": "opaque-subject"},
		},
		{
			name:        "does not remap over an existing to",
			allowlist:   `[{"from":"sub","to":"user.id"}]`,
			baggageVals: map[string]string{"sub": "from-sub", "user.id": "already"},
			want:        map[string]string{"sub": "from-sub", "user.id": "already"},
		},
		{
			name:      "maps metadata from onto to",
			allowlist: `[{"from":"sub","to":"user.id"},"gen_ai.conversation.id"]`,
			metadata:  map[string]any{"sub": "opaque-subject", "gen_ai.conversation.id": "sess-1"},
			want:      map[string]string{"user.id": "opaque-subject", "gen_ai.conversation.id": "sess-1"},
		},
		{
			name:      "skips non-scalars",
			allowlist: "value",
			metadata:  map[string]any{"value": map[string]any{"nested": true}},
			want:      map[string]string{},
		},
		{
			name:      "renders scalar metadata",
			allowlist: "flag,count",
			metadata:  map[string]any{"flag": true, "count": float64(3)},
			want:      map[string]string{"flag": "true", "count": "3"},
		},
		{
			name:      "strips control characters",
			allowlist: "note",
			metadata:  map[string]any{"note": "line\nbreak\tand\x00nul"},
			want:      map[string]string{"note": "linebreakandnul"},
		},
		{
			name:      "drops keys with whitespace",
			allowlist: "bad key,user.id",
			metadata:  map[string]any{"bad key": "x", "user.id": "ok"},
			want:      map[string]string{"user.id": "ok"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAllowlist(t, tt.allowlist)
			got := baggageValues(ContextWithPromotedMetadata(baggageContext(t, tt.baggageVals), tt.metadata))
			if len(got) != len(tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
			for key, want := range tt.want {
				if got[key] != want {
					t.Errorf("%s: got %q, want %q", key, got[key], want)
				}
			}
		})
	}
}

func TestContextWithPromotedMetadata_CapsAllowlist(t *testing.T) {
	keys := make([]string, maxContextKeys+4)
	metadata := map[string]any{}
	for i := range keys {
		keys[i] = "k" + strconv.Itoa(i)
		metadata[keys[i]] = "v"
	}
	setAllowlist(t, strings.Join(keys, ","))
	got := baggageValues(ContextWithPromotedMetadata(context.Background(), metadata))
	if len(got) != maxContextKeys {
		t.Fatalf("promoted %d keys, want %d", len(got), maxContextKeys)
	}
}

func TestAllowedBaggageCopyFilter(t *testing.T) {
	if AllowedBaggageCopyFilter() != nil {
		t.Fatal("empty allowlist must not install a baggagecopy filter")
	}

	setAllowlist(t, `[{"from":"sub","to":"user.id"},"thread_id"]`)
	filter := AllowedBaggageCopyFilter()
	if filter == nil {
		t.Fatal("expected a filter")
	}
	if !filter(mustMember(t, "user.id", "x")) {
		t.Error("user.id should be copied")
	}
	if !filter(mustMember(t, "thread_id", "x")) {
		t.Error("thread_id should be copied")
	}
	if filter(mustMember(t, "sub", "x")) {
		t.Error("source-only key sub should not be copied")
	}
	if filter(mustMember(t, "secret", "x")) {
		t.Error("non-allowlisted key should not be copied")
	}
}

func TestCallerContextLandsOnEverySpan(t *testing.T) {
	setAllowlist(t, "user.id")

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSpanProcessor(baggagecopy.NewSpanProcessor(AllowedBaggageCopyFilter())),
		sdktrace.WithSpanProcessor(kagentAttributesSpanProcessor{}),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx := ContextWithPromotedMetadata(baggageContext(t, map[string]string{"user.id": "opaque-subject"}), nil)
	ctx = SetKAgentSpanAttributes(ctx, map[string]string{"kagent.user_id": "runtime-user"})
	tracer := tp.Tracer("test")
	ctx, root := tracer.Start(ctx, "invocation")
	_, tool := tracer.Start(ctx, "tool")
	_, model := tracer.Start(ctx, "generate_content")
	model.End()
	tool.End()
	root.End()

	for _, name := range []string{"invocation", "tool", "generate_content"} {
		attrs := spanAttributesByName(t, exporter.GetSpans(), name)
		if got := attrs["user.id"].AsString(); got != "opaque-subject" {
			t.Errorf("%s user.id = %q, want opaque-subject", name, got)
		}
		if got := attrs["kagent.user_id"].AsString(); got != "runtime-user" {
			t.Errorf("%s kagent.user_id = %q, want runtime-user", name, got)
		}
	}
}

func TestMetadataPromotionReachesGenerateContent(t *testing.T) {
	setAllowlist(t, `[{"from":"sub","to":"user.id"}]`)

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSpanProcessor(baggagecopy.NewSpanProcessor(AllowedBaggageCopyFilter())),
		sdktrace.WithSpanProcessor(kagentAttributesSpanProcessor{}),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx := ContextWithPromotedMetadata(context.Background(), map[string]any{"sub": "opaque-subject"})
	ctx = SetKAgentSpanAttributes(ctx, map[string]string{"gen_ai.conversation.id": "sess-runtime"})
	tracer := tp.Tracer("test")
	ctx, root := tracer.Start(ctx, "invocation")
	_, model := tracer.Start(ctx, "generate_content")
	model.End()
	root.End()

	for _, name := range []string{"invocation", "generate_content"} {
		attrs := spanAttributesByName(t, exporter.GetSpans(), name)
		if got := attrs["user.id"].AsString(); got != "opaque-subject" {
			t.Errorf("%s user.id = %q, want opaque-subject", name, got)
		}
		if got := attrs["gen_ai.conversation.id"].AsString(); got != "sess-runtime" {
			t.Errorf("%s gen_ai.conversation.id = %q, want sess-runtime", name, got)
		}
		if _, exists := attrs["a2a.message.metadata.sub"]; exists {
			t.Errorf("%s unexpectedly stamped a2a.message.metadata.sub", name)
		}
	}
}

func TestRuntimeAttributesWinOverAllowlistedBaggage(t *testing.T) {
	setAllowlist(t, "gen_ai.conversation.id")

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSpanProcessor(baggagecopy.NewSpanProcessor(AllowedBaggageCopyFilter())),
		sdktrace.WithSpanProcessor(kagentAttributesSpanProcessor{}),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx := baggageContext(t, map[string]string{"gen_ai.conversation.id": "from-caller"})
	ctx = SetKAgentSpanAttributes(ctx, map[string]string{"gen_ai.conversation.id": "from-runtime"})
	tracer := tp.Tracer("test")
	_, span := tracer.Start(ctx, "generate_content")
	span.End()

	attrs := spanAttributesByName(t, exporter.GetSpans(), "generate_content")
	if got := attrs["gen_ai.conversation.id"].AsString(); got != "from-runtime" {
		t.Fatalf("gen_ai.conversation.id = %q, want runtime value to win", got)
	}
}

func mustMember(t *testing.T, key, value string) baggage.Member {
	t.Helper()
	member, err := baggage.NewMember(key, value)
	if err != nil {
		t.Fatal(err)
	}
	return member
}

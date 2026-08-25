package telemetry

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/baggage"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

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

func TestCallerContextAttributes(t *testing.T) {
	tests := []struct {
		name        string
		allowlist   string
		baggageVals map[string]string
		metadata    map[string]any
		want        map[string]string
	}{
		{
			name:        "disabled by default",
			allowlist:   "",
			baggageVals: map[string]string{"user.email": "ada@example.com"},
			metadata:    map[string]any{"thread_id": "T123"},
			want:        nil,
		},
		{
			name:        "promotes allowlisted baggage",
			allowlist:   "user.email,user.name",
			baggageVals: map[string]string{"user.email": "ada@example.com", "user.name": "Ada"},
			want: map[string]string{
				"kagent.context.user.email": "ada@example.com",
				"kagent.context.user.name":  "Ada",
			},
		},
		{
			name:      "promotes allowlisted message metadata",
			allowlist: "thread_id,channel",
			metadata:  map[string]any{"thread_id": "1717171.4242", "channel": "C0AB1"},
			want: map[string]string{
				"kagent.context.thread_id": "1717171.4242",
				"kagent.context.channel":   "C0AB1",
			},
		},
		{
			name:        "message metadata overrides baggage",
			allowlist:   "user.email",
			baggageVals: map[string]string{"user.email": "from-baggage@example.com"},
			metadata:    map[string]any{"user.email": "from-metadata@example.com"},
			want:        map[string]string{"kagent.context.user.email": "from-metadata@example.com"},
		},
		{
			name:        "ignores keys outside the allowlist",
			allowlist:   "thread_id",
			baggageVals: map[string]string{"secret.token": "s3cret"},
			metadata:    map[string]any{"thread_id": "T1", "customer.pan": "4111111111111111"},
			want:        map[string]string{"kagent.context.thread_id": "T1"},
		},
		{
			name:      "renders scalar metadata types",
			allowlist: "count,ratio,enabled",
			metadata: map[string]any{
				"count":   int64(7),
				"ratio":   float64(2.5),
				"enabled": true,
			},
			want: map[string]string{
				"kagent.context.count":   "7",
				"kagent.context.ratio":   "2.5",
				"kagent.context.enabled": "true",
			},
		},
		{
			name:      "skips non-scalar and empty metadata values",
			allowlist: "nested,list,blank",
			metadata: map[string]any{
				"nested": map[string]any{"a": "b"},
				"list":   []string{"a"},
				"blank":  "",
			},
			want: nil,
		},
		{
			name:      "strips control characters",
			allowlist: "note",
			metadata:  map[string]any{"note": "line\nbreak\tand\x00nul"},
			want:      map[string]string{"kagent.context.note": "linebreakandnul"},
		},
		{
			name:      "ignores allowlist entries that are not valid attribute keys",
			allowlist: "good, bad key ,\tanother\tbad",
			metadata:  map[string]any{"good": "yes", "bad key": "no"},
			want:      map[string]string{"kagent.context.good": "yes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(traceContextKeysEnvVar, tt.allowlist)

			got := CallerContextAttributes(baggageContext(t, tt.baggageVals), tt.metadata)

			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for key, want := range tt.want {
				if got[key] != want {
					t.Errorf("%s = %q, want %q", key, got[key], want)
				}
			}
		})
	}
}

func TestCallerContextAttributes_TruncatesLongValues(t *testing.T) {
	t.Setenv(traceContextKeysEnvVar, "note")

	got := CallerContextAttributes(context.Background(), map[string]any{
		"note": strings.Repeat("a", maxContextValueLength*2),
	})

	if len(got["kagent.context.note"]) != maxContextValueLength {
		t.Errorf("value length = %d, want %d", len(got["kagent.context.note"]), maxContextValueLength)
	}
}

func TestAllowedContextKeys_CapsListLength(t *testing.T) {
	keys := make([]string, 0, maxContextKeys*2)
	for i := range maxContextKeys * 2 {
		keys = append(keys, "key"+strconv.Itoa(i))
	}
	t.Setenv(traceContextKeysEnvVar, strings.Join(keys, ","))

	if got := len(allowedContextKeys()); got != maxContextKeys {
		t.Errorf("allowlist length = %d, want %d", got, maxContextKeys)
	}
}

func TestAllowedContextKeys_DropsOverLongAndDuplicateKeys(t *testing.T) {
	t.Setenv(traceContextKeysEnvVar, "a,a,"+strings.Repeat("b", maxContextKeyLength+1)+",c")

	got := allowedContextKeys()

	want := []string{"a", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, key := range want {
		if got[i] != key {
			t.Errorf("key %d = %q, want %q", i, got[i], key)
		}
	}
}

// Langfuse and comparable backends filter on attributes present on each span,
// so the promoted values must reach descendants, not just the root span.
func TestCallerContextAttributes_ReachEverySpan(t *testing.T) {
	t.Setenv(traceContextKeysEnvVar, "user.email")

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSpanProcessor(kagentAttributesSpanProcessor{}),
	)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	ctx := baggageContext(t, map[string]string{"user.email": "ada@example.com"})
	ctx = SetKAgentSpanAttributes(ctx, CallerContextAttributes(ctx, nil))

	tracer := tp.Tracer("test")
	ctx, root := tracer.Start(ctx, "root")
	ctx, tool := tracer.Start(ctx, "execute_tool")
	_, model := tracer.Start(ctx, "generate_content")
	model.End()
	tool.End()
	root.End()

	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(spans))
	}
	for _, name := range []string{"root", "execute_tool", "generate_content"} {
		attrs := spanAttributesByName(t, spans, name)
		if got := attrs["kagent.context.user.email"].AsString(); got != "ada@example.com" {
			t.Errorf("span %q: kagent.context.user.email = %q, want %q", name, got, "ada@example.com")
		}
	}
}

func TestCallerContextAttributes_DisabledLeavesSpansUnchanged(t *testing.T) {
	t.Setenv(traceContextKeysEnvVar, "")

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSpanProcessor(kagentAttributesSpanProcessor{}),
	)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	ctx := baggageContext(t, map[string]string{"user.email": "ada@example.com"})
	ctx = SetKAgentSpanAttributes(ctx, CallerContextAttributes(ctx, map[string]any{"thread_id": "T1"}))

	_, span := tp.Tracer("test").Start(ctx, "root")
	span.End()

	for _, attr := range exporter.GetSpans()[0].Attributes {
		if strings.HasPrefix(string(attr.Key), contextAttributePrefix) {
			t.Errorf("unexpected promoted attribute %q", attr.Key)
		}
	}
}

// The kagent.context. prefix is what makes caller-supplied data unable to
// shadow a semantic convention attribute, even if an operator allowlists one.
func TestCallerContextAttributes_CannotShadowSemanticConventions(t *testing.T) {
	t.Setenv(traceContextKeysEnvVar, "service.name")

	got := CallerContextAttributes(context.Background(), map[string]any{"service.name": "impostor"})

	if _, shadowed := got["service.name"]; shadowed {
		t.Error("service.name must not be settable by a caller")
	}
	if got["kagent.context.service.name"] != "impostor" {
		t.Errorf("got %v, want the value namespaced under %q", got, contextAttributePrefix)
	}
}

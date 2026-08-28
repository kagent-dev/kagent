package telemetry

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

func hmacSHA256Hex(t *testing.T, key, value string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestCallerContextAttributes(t *testing.T) {
	tests := []struct {
		name        string
		allowlist   string
		hashKey     string
		baggageVals map[string]string
		metadata    map[string]any
		want        map[string]string
	}{
		{
			name:        "empty allowlist disables promotion",
			allowlist:   "",
			baggageVals: map[string]string{"sub": "opaque-subject"},
			metadata:    map[string]any{"thread_id": "T123"},
			want:        nil,
		},
		{
			name:        "promotes allowlisted baggage",
			allowlist:   "sub,thread_id",
			baggageVals: map[string]string{"sub": "opaque-subject", "thread_id": "T123"},
			want: map[string]string{
				"kagent.context.sub":       "opaque-subject",
				"kagent.context.thread_id": "T123",
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
			allowlist:   "sub",
			baggageVals: map[string]string{"sub": "from-baggage"},
			metadata:    map[string]any{"sub": "from-metadata"},
			want:        map[string]string{"kagent.context.sub": "from-metadata"},
		},
		{
			name:        "ignores keys outside the allowlist",
			allowlist:   "thread_id",
			baggageVals: map[string]string{"secret.token": "s3cret"},
			metadata:    map[string]any{"thread_id": "T1", "extra": "nope"},
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
		{
			// Registry names pass through unprefixed so operators can use
			// semantic convention attributes instead of inventing new ones.
			name:      "registry attributes stay unprefixed",
			allowlist: "user.id,enduser.id,session.id,channel",
			metadata: map[string]any{
				"user.id":    "opaque-subject",
				"enduser.id": "end-user",
				"session.id": "sess-1",
				"channel":    "C0AB1",
			},
			want: map[string]string{
				"user.id":                "opaque-subject",
				"enduser.id":             "end-user",
				"session.id":             "sess-1",
				"kagent.context.channel": "C0AB1",
			},
		},
		{
			// session.id is the registry name; other session.* keys are not.
			name:      "session.id is unprefixed but session.foo is not",
			allowlist: "session.id,session.foo",
			metadata:  map[string]any{"session.id": "sess-1", "session.foo": "other"},
			want: map[string]string{
				"session.id":                 "sess-1",
				"kagent.context.session.foo": "other",
			},
		},
		{
			name:      "maps source keys onto registry and kagent names",
			allowlist: `[{"from":"sub","to":"user.id"},{"from":"thread_id","to":"kagent.thread_id"},"channel"]`,
			metadata: map[string]any{
				"sub":       "opaque-subject",
				"thread_id": "T123",
				"channel":   "C0AB1",
			},
			want: map[string]string{
				"user.id":                "opaque-subject",
				"kagent.thread_id":       "T123",
				"kagent.context.channel": "C0AB1",
			},
		},
		{
			name:      "invalid JSON allowlist promotes nothing",
			allowlist: `[{"from":"sub"`,
			metadata:  map[string]any{"sub": "opaque-subject"},
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(traceContextKeysEnvVar, tt.allowlist)
			if tt.hashKey != "" {
				t.Setenv(traceContextHashKeyEnvVar, tt.hashKey)
			}

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

func TestCallerContextAttributes_HashesWithHMACSHA256(t *testing.T) {
	const key = "test-hmac-key"
	t.Setenv(traceContextKeysEnvVar, `[{"from":"email","to":"user.hash","hash":"hmac-sha256"}]`)
	t.Setenv(traceContextHashKeyEnvVar, key)

	got := CallerContextAttributes(context.Background(), map[string]any{
		"email": "ada@example.com",
	})

	want := hmacSHA256Hex(t, key, "ada@example.com")
	if got["user.hash"] != want {
		t.Errorf("user.hash = %q, want %q", got["user.hash"], want)
	}
	for name, value := range got {
		if strings.Contains(value, "@example.com") {
			t.Errorf("plaintext leaked onto %s", name)
		}
	}
}

func TestCallerContextAttributes_HashWithoutKeyEmitsNothing(t *testing.T) {
	// Missing HMAC key must not fall back to putting the original value on the span.
	t.Setenv(traceContextKeysEnvVar, `[{"from":"email","to":"user.hash","hash":"hmac-sha256"}]`)
	t.Setenv(traceContextHashKeyEnvVar, "")

	got := CallerContextAttributes(context.Background(), map[string]any{
		"email": "ada@example.com",
	})
	if got != nil {
		t.Errorf("got %v, want nothing when the HMAC key is unset", got)
	}
}

func TestCallerContextAttributes_UnknownHashEmitsNothing(t *testing.T) {
	t.Setenv(traceContextKeysEnvVar, `[{"from":"email","to":"user.hash","hash":"md5"}]`)
	t.Setenv(traceContextHashKeyEnvVar, "test-hmac-key")

	got := CallerContextAttributes(context.Background(), map[string]any{
		"email": "ada@example.com",
	})
	if got != nil {
		t.Errorf("got %v, want nothing for an unsupported hash", got)
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

func TestAllowedContextMappings_CapsListLength(t *testing.T) {
	keys := make([]string, 0, maxContextKeys*2)
	for i := range maxContextKeys * 2 {
		keys = append(keys, "key"+strconv.Itoa(i))
	}
	t.Setenv(traceContextKeysEnvVar, strings.Join(keys, ","))

	if got := len(allowedContextMappings()); got != maxContextKeys {
		t.Errorf("allowlist length = %d, want %d", got, maxContextKeys)
	}
}

func TestAllowedContextMappings_DropsOverLongAndDuplicateKeys(t *testing.T) {
	t.Setenv(traceContextKeysEnvVar, "a,a,"+strings.Repeat("b", maxContextKeyLength+1)+",c")

	got := allowedContextMappings()

	want := []string{"a", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %v", got, want)
	}
	for i, key := range want {
		if got[i].source != key {
			t.Errorf("key %d = %q, want %q", i, got[i].source, key)
		}
	}
}

// Langfuse and comparable backends filter on attributes present on each span,
// so the promoted values must reach descendants, not just the root span.
func TestCallerContextAttributes_ReachEverySpan(t *testing.T) {
	t.Setenv(traceContextKeysEnvVar, `[{"from":"sub","to":"user.id"}]`)

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSpanProcessor(kagentAttributesSpanProcessor{}),
	)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	ctx := baggageContext(t, map[string]string{"sub": "opaque-subject"})
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
		if got := attrs["user.id"].AsString(); got != "opaque-subject" {
			t.Errorf("span %q: user.id = %q, want %q", name, got, "opaque-subject")
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

	ctx := baggageContext(t, map[string]string{"sub": "opaque-subject"})
	ctx = SetKAgentSpanAttributes(ctx, CallerContextAttributes(ctx, map[string]any{"thread_id": "T1"}))

	_, span := tp.Tracer("test").Start(ctx, "root")
	span.End()

	for _, attr := range exporter.GetSpans()[0].Attributes {
		key := string(attr.Key)
		if strings.HasPrefix(key, contextAttributePrefix) || key == "user.id" {
			t.Errorf("unexpected promoted attribute %q", attr.Key)
		}
	}
}

// Custom keys still cannot shadow a semantic convention attribute such as
// service.name. Registry names are the documented exception.
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

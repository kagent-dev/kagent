package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/adk/v2/memory"
	adksession "google.golang.org/adk/v2/session"
)

// recordSpans installs an in-memory span recorder as the global tracer provider for
// the duration of the test and returns it so the caller can assert on emitted spans.
func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return sr
}

func spanByName(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

func attrValue(span sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

func assertAttr(t *testing.T, span sdktrace.ReadOnlySpan, key, want string) {
	t.Helper()
	v, ok := attrValue(span, key)
	if !ok {
		t.Fatalf("span %q missing attribute %q", span.Name(), key)
	}
	if v.AsString() != want {
		t.Errorf("span %q attribute %q = %q, want %q", span.Name(), key, v.AsString(), want)
	}
}

func TestSearchMemory_EmitsReadSpan(t *testing.T) {
	tests := []struct {
		name          string
		results       []searchResultItem
		wantInjection string
		wantCount     int64
	}{
		{
			name:          "hits_injected",
			results:       []searchResultItem{{ID: "m1", Content: "fact", Score: 0.9}},
			wantInjection: telemetry.MemoryInjectionInjected,
			wantCount:     1,
		},
		{
			name:          "no_hits_filtered",
			results:       []searchResultItem{},
			wantInjection: telemetry.MemoryInjectionFiltered,
			wantCount:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := recordSpans(t)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.results)
			}))
			defer server.Close()

			embClient, embServer := newMockEmbeddingClient(t)
			defer embServer.Close()

			svc := &KagentMemoryService{
				agentName:       "test-agent",
				apiURL:          server.URL,
				client:          server.Client(),
				ttlDays:         15,
				embeddingClient: embClient,
			}

			if _, err := svc.SearchMemory(context.Background(), &memory.SearchRequest{Query: "q", UserID: "u1"}); err != nil {
				t.Fatalf("SearchMemory() error = %v", err)
			}

			span := spanByName(sr.Ended(), telemetry.SpanMemoryRead)
			if span == nil {
				t.Fatalf("expected a %q span", telemetry.SpanMemoryRead)
			}
			assertAttr(t, span, telemetry.AttrMemoryOperation, telemetry.MemoryOperationPrefetch)
			assertAttr(t, span, telemetry.AttrMemoryScope, telemetry.MemoryScopeUser)
			assertAttr(t, span, telemetry.AttrMemoryIndexRef, "test-agent")
			assertAttr(t, span, telemetry.AttrMemorySUTName, "kagent")
			assertAttr(t, span, telemetry.AttrMemorySUTStoreBackend, "pgvector")
			assertAttr(t, span, telemetry.AttrMemoryInjectionResult, tt.wantInjection)

			v, ok := attrValue(span, telemetry.AttrMemoryItemCount)
			if !ok || v.AsInt64() != tt.wantCount {
				t.Errorf("%s = %v (ok=%v), want %d", telemetry.AttrMemoryItemCount, v.AsInt64(), ok, tt.wantCount)
			}
		})
	}
}

func TestSaveMemoryItem_EmitsWriteSpan(t *testing.T) {
	sr := recordSpans(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	embClient, embServer := newMockEmbeddingClient(t)
	defer embServer.Close()

	svc := &KagentMemoryService{
		agentName:       "test-agent",
		apiURL:          server.URL,
		client:          server.Client(),
		ttlDays:         15,
		embeddingClient: embClient,
	}

	if err := svc.SaveMemoryItem(context.Background(), "user1", "the secret code is BLUE-PANGOLIN-42"); err != nil {
		t.Fatalf("SaveMemoryItem() error = %v", err)
	}

	span := spanByName(sr.Ended(), telemetry.SpanMemoryWrite)
	if span == nil {
		t.Fatalf("expected a %q span from the save_memory path", telemetry.SpanMemoryWrite)
	}
	assertAttr(t, span, telemetry.AttrMemoryOperation, telemetry.MemoryOperationSave)
	// Explicit saves store content verbatim -> source=user, no summarization.
	assertAttr(t, span, telemetry.AttrMemorySource, telemetry.MemorySourceUser)
	assertAttr(t, span, telemetry.AttrMemoryScope, telemetry.MemoryScopeUser)
	assertAttr(t, span, telemetry.AttrMemoryIndexRef, "test-agent")
	assertAttr(t, span, telemetry.AttrMemorySUTStoreBackend, "pgvector")

	if v, ok := attrValue(span, telemetry.AttrMemoryItemCount); !ok || v.AsInt64() != 1 {
		t.Errorf("%s = %v (ok=%v), want 1", telemetry.AttrMemoryItemCount, v.AsInt64(), ok)
	}

	// The verbatim save path must NOT emit a consolidate span.
	if c := spanByName(sr.Ended(), telemetry.SpanMemoryConsolidate); c != nil {
		t.Errorf("save_memory path unexpectedly emitted a %q span", telemetry.SpanMemoryConsolidate)
	}
}

func TestAddSessionToMemory_EmitsWriteSpan(t *testing.T) {
	sr := recordSpans(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	embClient, embServer := newMockEmbeddingClient(t)
	defer embServer.Close()

	svc := &KagentMemoryService{
		agentName:       "test-agent",
		apiURL:          server.URL,
		client:          server.Client(),
		ttlDays:         15,
		embeddingClient: embClient,
	}

	session := newMockSession("sess1", "user1", []*adksession.Event{
		newMockEvent("user", "remember this"),
	})
	if err := svc.AddSessionToMemory(context.Background(), session); err != nil {
		t.Fatalf("AddSessionToMemory() error = %v", err)
	}

	span := spanByName(sr.Ended(), telemetry.SpanMemoryWrite)
	if span == nil {
		t.Fatalf("expected a %q span", telemetry.SpanMemoryWrite)
	}
	assertAttr(t, span, telemetry.AttrMemoryOperation, telemetry.MemoryOperationSave)
	// No model configured, so raw session content is stored -> source=user.
	assertAttr(t, span, telemetry.AttrMemorySource, telemetry.MemorySourceUser)
	assertAttr(t, span, telemetry.AttrMemorySUTArchitecture, "vector")
}

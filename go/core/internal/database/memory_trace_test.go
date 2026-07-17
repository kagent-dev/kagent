package database

import (
	"context"
	"testing"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestSearchAgentMemory_EmitsDBSpan verifies that the controller's pgvector
// search emits a `db.memory.search` child span carrying the expected non-PII
// DB semantic-convention attributes — and, crucially, that it never leaks the
// raw embedding vector or query/content text onto span attributes
// (observability PII guardrail).
func TestSearchAgentMemory_EmitsDBSpan(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prevTracer := dbTracer
	dbTracer = tp.Tracer("kagent.controller.database")
	t.Cleanup(func() {
		dbTracer = prevTracer
		_ = tp.Shutdown(context.Background())
	})

	// Empty table → zero results; the span is emitted regardless of row count.
	embedding := pgvector.NewVector(make([]float32, memoryVectorDim))
	_, err := client.SearchAgentMemory(context.Background(), "trace-agent", "trace-user", embedding, 5)
	require.NoError(t, err)

	span := findSpan(t, exporter.GetSpans(), "db.memory.search")

	gotAttrs := attrMap(span)
	assert.Equal(t, "postgresql", gotAttrs["db.system.name"])
	assert.Equal(t, "SELECT", gotAttrs["db.operation.name"])
	assert.Equal(t, "memory", gotAttrs["db.collection.name"])
	assert.Equal(t, int64(5), gotAttrs["memory.limit"])
	assert.Equal(t, int64(0), gotAttrs["memory.item.count"], "empty table should yield zero rows")

	assertNoPII(t, span)
}

// TestStoreAgentMemory_EmitsDBSpan verifies the insert path emits a
// `db.memory.insert` span with a per-item count and no PII.
func TestStoreAgentMemory_EmitsDBSpan(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prevTracer := dbTracer
	dbTracer = tp.Tracer("kagent.controller.database")
	t.Cleanup(func() {
		dbTracer = prevTracer
		_ = tp.Shutdown(context.Background())
	})

	err := client.StoreAgentMemory(context.Background(), &dbpkg.Memory{
		AgentName: "trace-agent",
		UserID:    "trace-user",
		Content:   "a secret the span must never carry",
		Embedding: pgvector.NewVector(make([]float32, memoryVectorDim)),
		Metadata:  "{}",
	})
	require.NoError(t, err)

	span := findSpan(t, exporter.GetSpans(), "db.memory.insert")

	gotAttrs := attrMap(span)
	assert.Equal(t, "postgresql", gotAttrs["db.system.name"])
	assert.Equal(t, "INSERT", gotAttrs["db.operation.name"])
	assert.Equal(t, "memory", gotAttrs["db.collection.name"])
	assert.Equal(t, int64(1), gotAttrs["memory.item.count"])

	assertNoPII(t, span)
}

// memoryVectorDim is the embedding dimension enforced by the memory schema.
const memoryVectorDim = 768

func findSpan(t *testing.T, spans tracetest.SpanStubs, name string) tracetest.SpanStub {
	t.Helper()
	for _, s := range spans {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no span named %q found (got %d spans)", name, len(spans))
	return tracetest.SpanStub{}
}

func attrMap(span tracetest.SpanStub) map[string]interface{} {
	m := make(map[string]interface{})
	for _, a := range span.Attributes {
		m[string(a.Key)] = a.Value.AsInterface()
	}
	return m
}

// assertNoPII fails if any span attribute key hints at raw content, query text,
// or the embedding vector — the cardinality/PII footguns the guardrail forbids.
func assertNoPII(t *testing.T, span tracetest.SpanStub) {
	t.Helper()
	forbidden := []string{"embedding", "vector", "content", "query", "text"}
	for _, a := range span.Attributes {
		key := string(a.Key)
		for _, bad := range forbidden {
			assert.NotContains(t, key, bad, "span attribute %q must not expose %q", key, bad)
		}
	}
}

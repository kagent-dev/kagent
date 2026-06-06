package compaction

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

func makeEvent(invID, author, text string) *adksession.Event {
	e := &adksession.Event{
		ID:           uuid.NewString(),
		InvocationID: invID,
		Author:       author,
		Timestamp:    time.Now(),
	}
	e.LLMResponse = model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: []*genai.Part{{Text: text}},
		},
	}
	return e
}

func TestNew_NilConfig(t *testing.T) {
	c := New(nil, logr.Discard())
	if c != nil {
		t.Fatal("expected nil compactor for nil config")
	}
}

func TestNew_WithConfig(t *testing.T) {
	cfg := &Config{CompactionInterval: 5, OverlapSize: 2}
	c := New(cfg, logr.Discard())
	if c == nil {
		t.Fatal("expected non-nil compactor")
	}
}

func TestGroupByInvocation_Basic(t *testing.T) {
	events := []*adksession.Event{
		makeEvent("inv1", "user", "hello"),
		makeEvent("inv1", "agent", "hi"),
		makeEvent("inv2", "user", "how are you"),
		makeEvent("inv2", "agent", "good"),
		makeEvent("inv3", "user", "bye"),
	}
	groups := groupByInvocation(events)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if groups[0].invocationID != "inv1" {
		t.Errorf("expected inv1, got %s", groups[0].invocationID)
	}
	if len(groups[0].events) != 2 {
		t.Errorf("expected 2 events in inv1, got %d", len(groups[0].events))
	}
}

func TestGroupByInvocation_EmptyID(t *testing.T) {
	events := []*adksession.Event{
		makeEvent("", "user", "hello"),
		makeEvent("", "agent", "hi"),
	}
	groups := groupByInvocation(events)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group for empty IDs, got %d", len(groups))
	}
}

func TestSerializeEvents(t *testing.T) {
	events := []*adksession.Event{
		makeEvent("inv1", "user", "hello world"),
	}
	text := serializeEvents(events)
	if text == "" {
		t.Fatal("expected non-empty serialized text")
	}
	if len(text) == 0 {
		t.Fatal("serialized text should not be empty")
	}
}

func TestEstimateTokens(t *testing.T) {
	events := []*adksession.Event{
		makeEvent("inv1", "user", "hello world"),
	}
	tokens := estimateTokens(events)
	if tokens <= 0 {
		t.Fatal("expected positive token estimate")
	}
}

func TestBuildSummaryEvent(t *testing.T) {
	e := buildSummaryEvent("this is a summary")
	if e == nil {
		t.Fatal("expected non-nil summary event")
	}
	if e.Author != summaryEventAuthor {
		t.Errorf("expected author %s, got %s", summaryEventAuthor, e.Author)
	}
	if e.Content == nil {
		t.Fatal("expected non-nil content")
	}
	if len(e.Content.Parts) == 0 || e.Content.Parts[0].Text != "this is a summary" {
		t.Error("unexpected summary content")
	}
}

func TestMaybeCompact_NilCompactor(t *testing.T) {
	var c *Compactor
	err := c.MaybeCompact(context.TODO(), nil, nil, 0)
	if err != nil {
		t.Fatalf("expected nil error from nil compactor, got %v", err)
	}
}

func TestCompact_BelowThreshold(t *testing.T) {
	cfg := &Config{CompactionInterval: 5, OverlapSize: 2}
	c := New(cfg, logr.Discard())

	// Only 2 invocations, threshold is 5 - compact should be a no-op.
	events := []*adksession.Event{
		makeEvent("inv1", "user", "hello"),
		makeEvent("inv2", "user", "world"),
	}
	invocations := groupByInvocation(events)
	if len(invocations) >= c.cfg.CompactionInterval {
		t.Fatal("test setup wrong: should be below threshold")
	}

	// compact returns nil without touching session when keepCount >= len(invocations).
	err := c.compact(context.TODO(), nil, nil, invocations, logr.Discard())
	if err != nil {
		t.Fatalf("expected nil error below threshold, got %v", err)
	}
}

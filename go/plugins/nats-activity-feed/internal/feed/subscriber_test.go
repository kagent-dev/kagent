package feed

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/adk/pkg/streaming"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func TestParseSubject(t *testing.T) {
	tests := []struct {
		name      string
		subject   string
		wantAgent string
		wantSess  string
	}{
		{"full subject", "agent.myagent.sess123.stream", "myagent", "sess123"},
		{"no session", "agent.myagent", "myagent", "unknown"},
		{"no agent", "agent", "unknown", "unknown"},
		{"extra parts", "agent.a.b.c.d", "a", "b"},
		{"empty", "", "unknown", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, sess := parseSubject(tt.subject)
			if agent != tt.wantAgent {
				t.Errorf("agent = %q, want %q", agent, tt.wantAgent)
			}
			if sess != tt.wantSess {
				t.Errorf("session = %q, want %q", sess, tt.wantSess)
			}
		})
	}
}

// mockHub captures broadcast events for testing.
type mockHub struct {
	events []FeedEvent
	ch     chan FeedEvent
}

func newMockHub() *mockHub {
	return &mockHub{ch: make(chan FeedEvent, 100)}
}

func (m *mockHub) Broadcast(event FeedEvent) {
	m.events = append(m.events, event)
	m.ch <- event
}

func startEmbeddedNATS(t *testing.T) *natsserver.Server {
	t.Helper()
	opts := &natsserver.Options{
		Host: "127.0.0.1",
		Port: -1, // random port
	}
	ns, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("failed to create NATS server: %v", err)
	}
	ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server not ready")
	}
	return ns
}

func TestSubscriber_Integration(t *testing.T) {
	ns := startEmbeddedNATS(t)
	defer ns.Shutdown()

	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()

	hub := newMockHub()
	sub, err := NewSubscriber(nc, "agent.>", hub)
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Close()

	// Publish a valid StreamEvent
	evt := streaming.StreamEvent{
		Type:      streaming.EventTypeToolStart,
		Data:      `{"name":"search"}`,
		Timestamp: 1234567890,
	}
	data, _ := json.Marshal(evt)
	if err := nc.Publish("agent.test-agent.session-1.stream", data); err != nil {
		t.Fatalf("publish: %v", err)
	}
	nc.Flush()

	select {
	case fe := <-hub.ch:
		if fe.Agent != "test-agent" {
			t.Errorf("Agent = %q, want %q", fe.Agent, "test-agent")
		}
		if fe.SessionID != "session-1" {
			t.Errorf("SessionID = %q, want %q", fe.SessionID, "session-1")
		}
		if fe.Type != "tool_start" {
			t.Errorf("Type = %q, want %q", fe.Type, "tool_start")
		}
		if fe.Data != `{"name":"search"}` {
			t.Errorf("Data = %q, want %q", fe.Data, `{"name":"search"}`)
		}
		if fe.Timestamp != 1234567890 {
			t.Errorf("Timestamp = %d, want 1234567890", fe.Timestamp)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for FeedEvent")
	}
}

func TestSubscriber_MalformedMessage(t *testing.T) {
	ns := startEmbeddedNATS(t)
	defer ns.Shutdown()

	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()

	hub := newMockHub()
	sub, err := NewSubscriber(nc, "agent.>", hub)
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Close()

	// Publish malformed data
	if err := nc.Publish("agent.bad.sess.stream", []byte("not json")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	nc.Flush()

	// Publish a valid event after the malformed one
	evt := streaming.StreamEvent{Type: streaming.EventTypeToken, Data: "hello", Timestamp: 999}
	data, _ := json.Marshal(evt)
	if err := nc.Publish("agent.good.sess.stream", data); err != nil {
		t.Fatalf("publish: %v", err)
	}
	nc.Flush()

	select {
	case fe := <-hub.ch:
		if fe.Agent != "good" {
			t.Errorf("Expected good agent event, got %q", fe.Agent)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out — malformed message may have blocked subscriber")
	}
}

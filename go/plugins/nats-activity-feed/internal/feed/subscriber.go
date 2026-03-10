package feed

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/kagent-dev/kagent/go/adk/pkg/streaming"
	"github.com/nats-io/nats.go"
)

// Broadcaster is the interface for broadcasting feed events.
type Broadcaster interface {
	Broadcast(event FeedEvent)
}

// Subscriber connects to NATS and forwards parsed events to a Broadcaster.
type Subscriber struct {
	conn    *nats.Conn
	sub     *nats.Subscription
	hub     Broadcaster
	subject string
}

// NewSubscriber creates a NATS subscriber that parses messages and broadcasts FeedEvents.
func NewSubscriber(nc *nats.Conn, subject string, hub Broadcaster) (*Subscriber, error) {
	s := &Subscriber{
		conn:    nc,
		hub:     hub,
		subject: subject,
	}

	sub, err := nc.Subscribe(subject, s.handleMessage)
	if err != nil {
		return nil, err
	}
	s.sub = sub
	return s, nil
}

// Close drains the subscription.
func (s *Subscriber) Close() error {
	if s.sub != nil {
		return s.sub.Drain()
	}
	return nil
}

func (s *Subscriber) handleMessage(msg *nats.Msg) {
	agent, sessionID := parseSubject(msg.Subject)

	var event streaming.StreamEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		log.Printf("WARN: failed to parse StreamEvent from %s: %v", msg.Subject, err)
		return
	}

	s.hub.Broadcast(FeedEvent{
		Agent:     agent,
		SessionID: sessionID,
		Subject:   msg.Subject,
		Type:      string(event.Type),
		Data:      event.Data,
		Timestamp: event.Timestamp,
	})
}

// parseSubject extracts agent name and session ID from a NATS subject.
// Expected format: agent.{agentName}.{sessionID}.stream
// Returns (agent, sessionID). Unknown parts default to "unknown".
func parseSubject(subject string) (string, string) {
	parts := strings.Split(subject, ".")
	agent := "unknown"
	sessionID := "unknown"

	if len(parts) >= 2 {
		agent = parts[1]
	}
	if len(parts) >= 3 {
		sessionID = parts[2]
	}
	return agent, sessionID
}

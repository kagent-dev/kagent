package streaming

import (
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
)

// StreamPublisher publishes streaming events to NATS subjects.
type StreamPublisher struct {
	conn *nats.Conn
}

// NewStreamPublisher creates a publisher backed by the given NATS connection.
func NewStreamPublisher(conn *nats.Conn) *StreamPublisher {
	return &StreamPublisher{conn: conn}
}

// PublishToken publishes an LLM token event to the given subject.
func (p *StreamPublisher) PublishToken(subject string, token *StreamEvent) error {
	return p.publish(subject, token)
}

// PublishToolProgress publishes a tool progress event to the given subject.
func (p *StreamPublisher) PublishToolProgress(subject string, event *StreamEvent) error {
	return p.publish(subject, event)
}

// PublishApprovalRequest publishes an HITL approval request to the given subject.
func (p *StreamPublisher) PublishApprovalRequest(subject string, req *ApprovalRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal approval request: %w", err)
	}
	event := NewStreamEvent(EventTypeApprovalRequest, string(data))
	return p.publish(subject, event)
}

func (p *StreamPublisher) publish(subject string, event *StreamEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal stream event: %w", err)
	}
	if err := p.conn.Publish(subject, data); err != nil {
		return fmt.Errorf("failed to publish to %s: %w", subject, err)
	}
	return nil
}

// StreamSubscriber subscribes to NATS subjects for streaming events.
type StreamSubscriber struct {
	conn *nats.Conn
}

// NewStreamSubscriber creates a subscriber backed by the given NATS connection.
func NewStreamSubscriber(conn *nats.Conn) *StreamSubscriber {
	return &StreamSubscriber{conn: conn}
}

// Subscribe subscribes to a NATS subject and calls the handler for each event.
func (s *StreamSubscriber) Subscribe(subject string, handler func(*StreamEvent)) (*nats.Subscription, error) {
	return s.conn.Subscribe(subject, func(msg *nats.Msg) {
		var event StreamEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			return
		}
		handler(&event)
	})
}

// NewNATSConnection creates a NATS connection to the given address.
func NewNATSConnection(addr string) (*nats.Conn, error) {
	conn, err := nats.Connect(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS at %s: %w", addr, err)
	}
	return conn, nil
}

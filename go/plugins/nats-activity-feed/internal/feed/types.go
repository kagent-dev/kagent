package feed

// FeedEvent wraps a StreamEvent with subject metadata for the UI.
type FeedEvent struct {
	Agent     string `json:"agent"`
	SessionID string `json:"sessionId"`
	Subject   string `json:"subject"`
	Type      string `json:"type"`
	Data      string `json:"data"`
	Timestamp int64  `json:"timestamp"`
}

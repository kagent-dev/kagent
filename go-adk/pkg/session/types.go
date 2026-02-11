package session

// Session represents a user session
type Session struct {
	ID      string                 `json:"id"`
	UserID  string                 `json:"user_id"`
	AppName string                 `json:"app_name"`
	State   map[string]any `json:"state"`
	Events  []any          `json:"events"`
}

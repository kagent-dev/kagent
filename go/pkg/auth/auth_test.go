package auth

import (
	"testing"
)

func TestPrincipalHasRequiredFields(t *testing.T) {
	p := Principal{
		User: User{
			ID:    "user123",
			Email: "user@example.com",
			Name:  "Test User",
		},
		Groups: []string{"admin", "developers"},
		Agent:  Agent{ID: "agent1"},
	}

	if p.User.ID != "user123" {
		t.Errorf("expected User.ID 'user123', got '%s'", p.User.ID)
	}
	if p.User.Email != "user@example.com" {
		t.Errorf("expected User.Email 'user@example.com', got '%s'", p.User.Email)
	}
	if p.User.Name != "Test User" {
		t.Errorf("expected User.Name 'Test User', got '%s'", p.User.Name)
	}
	if len(p.Groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(p.Groups))
	}
}

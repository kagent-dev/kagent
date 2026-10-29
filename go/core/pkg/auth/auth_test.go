package auth

import (
	"testing"
)

func TestPrincipalHasRequiredFields(t *testing.T) {
	p := Principal{
		ID: "user123",
		Claims: map[string]any{
			"sub":   "user123",
			"email": "user@example.com",
			"name":  "Test User",
		},
	}

	if p.ID != "user123" {
		t.Errorf("expected User.ID 'user123', got '%s'", p.ID)
	}
	if p.Claims["email"] != "user@example.com" {
		t.Errorf("expected Claims[email] 'user@example.com', got '%v'", p.Claims["email"])
	}
	if p.Claims["name"] != "Test User" {
		t.Errorf("expected Claims[name] 'Test User', got '%v'", p.Claims["name"])
	}
}

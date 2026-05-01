package auth

import (
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestCheckAgentGroupAccess_NoAnnotation_Denied(t *testing.T) {
	principal := auth.Principal{
		User:   auth.User{ID: "user1"},
		Groups: []string{"team-a"},
	}
	err := checkAgentGroupAccess(principal, map[string]string{})
	if err == nil {
		t.Error("expected denial when no annotation, got nil")
	}
}

func TestCheckAgentGroupAccess_EmptyAnnotation_Denied(t *testing.T) {
	principal := auth.Principal{
		User:   auth.User{ID: "user1"},
		Groups: []string{"team-a"},
	}
	err := checkAgentGroupAccess(principal, map[string]string{
		AllowedGroupsAnnotation: "",
	})
	if err == nil {
		t.Error("expected denial when empty annotation, got nil")
	}
}

func TestCheckAgentGroupAccess_PublicAnnotation_AllowsEveryone(t *testing.T) {
	principal := auth.Principal{
		User:   auth.User{ID: "user1"},
		Groups: []string{"random-group"},
	}
	err := checkAgentGroupAccess(principal, map[string]string{
		AllowedGroupsAnnotation: "public",
	})
	if err != nil {
		t.Errorf("expected nil for public agent, got %v", err)
	}
}

func TestCheckAgentGroupAccess_PublicWithOtherGroups(t *testing.T) {
	principal := auth.Principal{
		User:   auth.User{ID: "user1"},
		Groups: []string{"team-a"},
	}
	err := checkAgentGroupAccess(principal, map[string]string{
		AllowedGroupsAnnotation: "doctors,public,nurses",
	})
	if err != nil {
		t.Errorf("expected nil when public is in allowed groups, got %v", err)
	}
}

func TestCheckAgentGroupAccess_MatchingGroup(t *testing.T) {
	principal := auth.Principal{
		User:   auth.User{ID: "user1"},
		Groups: []string{"team-a", "team-b"},
	}
	err := checkAgentGroupAccess(principal, map[string]string{
		AllowedGroupsAnnotation: "team-b,team-c",
	})
	if err != nil {
		t.Errorf("expected nil for matching group, got %v", err)
	}
}

func TestCheckAgentGroupAccess_NoMatchingGroup(t *testing.T) {
	principal := auth.Principal{
		User:   auth.User{ID: "user1"},
		Groups: []string{"team-a"},
	}
	err := checkAgentGroupAccess(principal, map[string]string{
		AllowedGroupsAnnotation: "team-b,team-c",
	})
	if err == nil {
		t.Error("expected denial when no matching group, got nil")
	}
}

func TestCheckAgentGroupAccess_NoGroupsInJWT(t *testing.T) {
	principal := auth.Principal{
		User: auth.User{ID: "user1"},
	}
	err := checkAgentGroupAccess(principal, map[string]string{
		AllowedGroupsAnnotation: "team-a",
	})
	if err == nil {
		t.Error("expected denial when user has no groups, got nil")
	}
}

func TestCheckAgentGroupAccess_AdminBypassesNoAnnotation(t *testing.T) {
	principal := auth.Principal{
		User:   auth.User{ID: "admin-user"},
		Groups: []string{"admin"},
	}
	err := checkAgentGroupAccess(principal, map[string]string{})
	if err != nil {
		t.Errorf("expected nil for admin user with no annotation, got %v", err)
	}
}

func TestCheckAgentGroupAccess_AdminBypassesRestrictedAgent(t *testing.T) {
	principal := auth.Principal{
		User:   auth.User{ID: "admin-user"},
		Groups: []string{"admin"},
	}
	err := checkAgentGroupAccess(principal, map[string]string{
		AllowedGroupsAnnotation: "doctors",
	})
	if err != nil {
		t.Errorf("expected nil for admin user on restricted agent, got %v", err)
	}
}

func TestCheckAgentGroupAccess_AdminWithOtherGroups(t *testing.T) {
	principal := auth.Principal{
		User:   auth.User{ID: "admin-user"},
		Groups: []string{"developers", "admin"},
	}
	err := checkAgentGroupAccess(principal, map[string]string{
		AllowedGroupsAnnotation: "nurses",
	})
	if err != nil {
		t.Errorf("expected nil for admin user even without matching group, got %v", err)
	}
}

func TestExtractGroupsFromClaims(t *testing.T) {
	tests := []struct {
		name     string
		claims   map[string]any
		expected int
	}{
		{"nil claims", nil, 0},
		{"missing claim", map[string]any{}, 0},
		{"[]any groups", map[string]any{"groups": []any{"a", "b"}}, 2},
		{"[]string groups", map[string]any{"groups": []string{"a", "b", "c"}}, 3},
		{"wrong type", map[string]any{"groups": "not-a-list"}, 0},
		{"cognito groups", map[string]any{"cognito:groups": []any{"x", "y"}}, 2},
		{"keycloak realm_access roles", map[string]any{"realm_access": map[string]any{"roles": []any{"role1", "role2"}}}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGroupsFromClaims(tt.claims)
			if len(got) != tt.expected {
				t.Errorf("extractGroupsFromClaims() = %v (len %d), want len %d", got, len(got), tt.expected)
			}
		})
	}
}

func TestParseCSV(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"a", 1},
		{"a,b,c", 3},
		{" a , b , c ", 3},
		{"a,,b", 2},
	}
	for _, tt := range tests {
		got := parseCSV(tt.input)
		if len(got) != tt.expected {
			t.Errorf("parseCSV(%q) len = %d, want %d", tt.input, len(got), tt.expected)
		}
	}
}

func TestContainsString(t *testing.T) {
	if !containsString([]string{"a", "b", "c"}, "b") {
		t.Error("expected true")
	}
	if containsString([]string{"a", "b"}, "c") {
		t.Error("expected false")
	}
	if containsString(nil, "a") {
		t.Error("expected false for nil slice")
	}
}

func TestHasIntersection(t *testing.T) {
	tests := []struct {
		a, b     []string
		expected bool
	}{
		{[]string{"a"}, []string{"a"}, true},
		{[]string{"a"}, []string{"b"}, false},
		{[]string{"a", "b"}, []string{"b", "c"}, true},
		{nil, []string{"a"}, false},
		{[]string{"a"}, nil, false},
	}
	for _, tt := range tests {
		got := hasIntersection(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("hasIntersection(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.expected)
		}
	}
}

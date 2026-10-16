package auth

import (
	"net/http"
	"testing"
)

func TestAddHeadersSendsTheUsersGroups(t *testing.T) {
	service := NewKAgentTokenService("kagent/assistant")
	for _, test := range []struct {
		name, userID, groupsOf, want string
	}{
		{name: "the user's groups", userID: "alice", groupsOf: "alice", want: "team-a"},
		{name: "another user's groups", userID: "alice", groupsOf: "bob"},
		{name: "no user", groupsOf: "alice"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := WithUserGroups(WithUserID(t.Context(), test.userID), test.groupsOf, "team-a")
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://controller", nil)
			if err != nil {
				t.Fatal(err)
			}
			service.AddHeaders(req)
			if got := req.Header.Get(UserGroupsHeader); got != test.want {
				t.Fatalf("%s = %q, want %q", UserGroupsHeader, got, test.want)
			}
		})
	}
}

package taskstore

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/stretchr/testify/require"
)

func TestInsecureRuntimeIdentity(t *testing.T) {
	id := uuid.NewString()
	valid := "team-a/session-" + id + "/actor-uid"
	for _, test := range []struct {
		name   string
		values []string
		valid  bool
	}{
		{"valid", []string{valid}, true},
		{"missing", nil, false},
		{"duplicate", []string{valid, valid}, false},
		{"invalid session", []string{"team-a/session-invalid/actor-uid"}, false},
		{"missing namespace", []string{"/session-" + id + "/actor-uid"}, false},
		{"missing UID", []string{"team-a/session-" + id + "/"}, false},
		{"extra component", []string{valid + "/extra"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := http.Header{}
			for _, value := range test.values {
				headers.Add(apia2a.InsecureRuntimeIdentityHeader, value)
			}
			session, err := (&Authenticator{}).Authenticate(t.Context(), headers, nil)
			if !test.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, runtimeSession{sessionID: id, atespace: "team-a", actorUID: "actor-uid"}, session)
		})
	}
}

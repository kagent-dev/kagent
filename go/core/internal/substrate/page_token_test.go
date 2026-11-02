package substrate_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestPageTokensMatchPinnedSubstrateFormat(t *testing.T) {
	for _, tc := range []struct{ name, token, want string }{
		{"global actor", substrate.ActorPageToken("", "team", "actor"), `{"v":1,"kind":"actor","scope":"","last":["team","actor"]}`},
		{"scoped actor", substrate.ActorPageToken("team", "team", "actor"), `{"v":1,"kind":"actor","scope":"team","last":["actor"]}`},
		{"worker", substrate.WorkerPageToken("worker"), `{"v":1,"kind":"worker","scope":"","last":["worker"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := base64.StdEncoding.DecodeString(tc.token)
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(raw))
		})
	}
}

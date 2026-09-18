package substrate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActorHost(t *testing.T) {
	if got := ActorHost("kagent", "actor-1", ""); got != "actor-1.kagent.actors.resources.substrate.ate.dev" {
		t.Fatalf("ActorHost() = %q", got)
	}
}

func TestActorTargetFromHost(t *testing.T) {
	for _, tc := range []struct {
		name      string
		authority string
		want      string
	}{
		{name: "actor", authority: ActorHost("kagent", "actor-1", ""), want: "kagent/actor-1"},
		{name: "empty"},
		{name: "wrong suffix", authority: "actor-1.kagent.example.com"},
		{name: "missing actor", authority: ActorHost("kagent", "", "")},
		{name: "missing atespace", authority: ActorHost("", "actor-1", "")},
		{name: "extra label", authority: ActorHost("kagent.extra", "actor-1", "")},
		{name: "invalid actor", authority: ActorHost("kagent", "actor/other", "")},
		{name: "invalid atespace", authority: ActorHost("kagent/other", "actor-1", "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ActorTargetFromHost(tc.authority)
			if tc.want == "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

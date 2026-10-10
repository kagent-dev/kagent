package sandbox

import (
	"testing"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/stretchr/testify/require"
)

func TestParseSignal(t *testing.T) {
	for _, test := range []struct {
		name string
		want guestpb.Signal
	}{
		{"KILL", guestpb.Signal_SIGNAL_KILL},
		{"term", guestpb.Signal_SIGNAL_TERM},
		{"SIGINT", guestpb.Signal_SIGNAL_INT},
		{"SIGNAL_HUP", guestpb.Signal_SIGNAL_HUP},
		{"15", guestpb.Signal_SIGNAL_TERM},
	} {
		t.Run(test.name, func(t *testing.T) {
			signal, err := parseSignal(test.name)
			require.NoError(t, err)
			require.Equal(t, test.want, signal)
		})
	}
	for _, name := range []string{"", "0", "16", "UNSPECIFIED", "SIGNAL_UNSPECIFIED", "BOGUS"} {
		t.Run("rejects "+name, func(t *testing.T) {
			_, err := parseSignal(name)
			require.ErrorContains(t, err, "unknown signal")
		})
	}
}

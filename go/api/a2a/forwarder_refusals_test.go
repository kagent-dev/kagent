package a2a

import (
	"encoding/json"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
)

func TestForwarderRefusalsSurviveTheWire(t *testing.T) {
	message := a2atype.NewMessage(a2atype.MessageRoleAgent)
	SetForwarderRefusals(message, ForwarderRefusals{})
	require.NotContains(t, message.Metadata, ForwarderRefusalsMetadataKey, "nothing refused records nothing")

	want := ForwarderRefusals{Unauthorized: 3, UnknownServer: 1}
	SetForwarderRefusals(message, want)
	got, ok := ForwarderRefusalsOf(message)
	require.True(t, ok)
	require.Equal(t, want, got)

	// A stored task's metadata holds JSON numbers.
	data, err := json.Marshal(message)
	require.NoError(t, err)
	var decoded a2atype.Message
	require.NoError(t, json.Unmarshal(data, &decoded))
	got, ok = ForwarderRefusalsOf(&decoded)
	require.True(t, ok)
	require.Equal(t, want, got)
}

func TestForwarderRefusalsOfRejectsMalformedCounts(t *testing.T) {
	none, ok := ForwarderRefusalsOf(a2atype.NewMessage(a2atype.MessageRoleAgent))
	require.True(t, ok)
	require.True(t, none.IsZero())
	for name, value := range map[string]any{
		"not an object": "3",
		"negative":      map[string]any{"unauthorized": -1.0},
		"fractional":    map[string]any{"unknownServer": 1.5},
		"a string":      map[string]any{"refusedPath": "1"},
	} {
		message := a2atype.NewMessage(a2atype.MessageRoleAgent)
		message.SetMeta(ForwarderRefusalsMetadataKey, value)
		_, ok := ForwarderRefusalsOf(message)
		require.False(t, ok, name)
	}
}

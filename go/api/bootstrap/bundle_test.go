package bootstrap

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestBundleValidation(t *testing.T) {
	revision := strings.Repeat("a", 64)
	b, err := NewBundle(revision, []byte(`{"instruction":"日"}`), []byte(`{"name":"agent"}`))
	require.NoError(t, err)
	d := Descriptor{FormatVersion: FormatVersion, ControlPlaneURL: "http://controller:8083", Revision: revision, SHA256: b.Sha256}
	require.NoError(t, d.Verify(b))
	for _, field := range []string{"revision", "config", "card", "digest", "version"} {
		t.Run(field, func(t *testing.T) {
			changed := proto.CloneOf(b)
			switch field {
			case "revision":
				changed.Revision = strings.Repeat("b", 64)
			case "config":
				changed.ConfigJson = []byte(`{}`)
			case "card":
				changed.AgentCardJson = []byte(`{}`)
			case "digest":
				changed.Sha256 = strings.Repeat("0", 64)
			case "version":
				changed.FormatVersion++
			}
			require.Error(t, d.Verify(changed))
		})
	}
	// Given multibyte JSON, when a bundle is created, assert that UTF-8 bytes
	// rather than code points determine the payload budget.
	raw, err := json.Marshal(strings.Repeat("日", MaxPayloadBytes/3))
	require.NoError(t, err)
	_, err = NewBundle(revision, raw, []byte(`{}`))
	require.ErrorContains(t, err, "exceed")
	config := []byte(`"` + strings.Repeat("x", (256<<10)-4) + `"`)
	_, err = NewBundle(revision, config, []byte(`{}`))
	require.NoError(t, err)
	_, err = NewBundle(revision, append(config, ' '), []byte(`{}`))
	require.ErrorContains(t, err, "exceed")
}

func TestSharedDigestVector(t *testing.T) {
	raw, err := os.ReadFile("testdata/digest.json")
	require.NoError(t, err)
	var vector struct {
		Version  uint32 `json:"format_version"`
		Revision string `json:"revision"`
		Config   string `json:"config_json"`
		Card     string `json:"agent_card_json"`
		SHA256   string `json:"sha256"`
	}
	require.NoError(t, json.Unmarshal(raw, &vector))
	bundle, err := NewBundle(vector.Revision, []byte(vector.Config), []byte(vector.Card))
	require.NoError(t, err)
	require.Equal(t, vector.Version, bundle.FormatVersion)
	require.Equal(t, vector.SHA256, bundle.Sha256)
}

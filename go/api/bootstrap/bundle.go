// Package bootstrap defines immutable runtime configuration delivery.
package bootstrap

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"

	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
)

const (
	FormatVersion   uint32 = 1
	MaxPayloadBytes        = 256 << 10
	// MaxBundleBytes allows 1 KiB for protobuf metadata above the payload budget.
	MaxBundleBytes = MaxPayloadBytes + (1 << 10)
	Environment    = "KAGENT_BOOTSTRAP_JSON"
	Audience       = "kagent-bootstrap"
	Directory      = "/data/kagent/bootstrap"
)

// Descriptor pins the immutable response expected by a runtime.
type Descriptor struct {
	FormatVersion   uint32 `json:"format_version"`
	ControlPlaneURL string `json:"control_plane_url"`
	Revision        string `json:"revision"`
	SHA256          string `json:"sha256"`
}

func (d Descriptor) Validate() error {
	u, err := url.Parse(d.ControlPlaneURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid bootstrap control-plane URL")
	}
	if d.FormatVersion != FormatVersion || !isDigest(d.Revision) || !isDigest(d.SHA256) {
		return fmt.Errorf("invalid bootstrap descriptor version or digest")
	}
	return nil
}

func isDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && hex.EncodeToString(b) == s
}

// NewBundle preserves the exact compiler-owned payload bytes.
func NewBundle(revision string, config, card []byte) (*runtimev1.BootstrapServiceGetBootstrapConfigResponse, error) {
	b := &runtimev1.BootstrapServiceGetBootstrapConfigResponse{FormatVersion: FormatVersion, Revision: revision, ConfigJson: config, AgentCardJson: card}
	b.Sha256 = Digest(b)
	if err := ValidateBundle(b); err != nil {
		return nil, err
	}
	return b, nil
}

// Digest hashes the version followed by length-prefixed revision, config and card.
// Lengths are unsigned 64-bit big endian; the version is unsigned 32-bit big endian.
func Digest(b *runtimev1.BootstrapServiceGetBootstrapConfigResponse) string {
	h := sha256.New()
	_ = binary.Write(h, binary.BigEndian, b.GetFormatVersion())
	for _, field := range [][]byte{[]byte(b.GetRevision()), b.GetConfigJson(), b.GetAgentCardJson()} {
		_ = binary.Write(h, binary.BigEndian, uint64(len(field)))
		_, _ = h.Write(field)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func ValidateBundle(b *runtimev1.BootstrapServiceGetBootstrapConfigResponse) error {
	if b.GetFormatVersion() != FormatVersion || !isDigest(b.GetRevision()) {
		return fmt.Errorf("invalid bootstrap bundle version or revision")
	}
	if len(b.GetConfigJson())+len(b.GetAgentCardJson()) > MaxPayloadBytes {
		return fmt.Errorf("bootstrap configuration and Agent Card exceed %d bytes", MaxPayloadBytes)
	}
	if !json.Valid(b.GetConfigJson()) || !json.Valid(b.GetAgentCardJson()) {
		return fmt.Errorf("invalid bootstrap JSON payload")
	}
	if b.GetSha256() != Digest(b) {
		return fmt.Errorf("bootstrap bundle digest mismatch")
	}
	return nil
}

func (d Descriptor) Verify(b *runtimev1.BootstrapServiceGetBootstrapConfigResponse) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := ValidateBundle(b); err != nil {
		return err
	}
	if b.GetRevision() != d.Revision || b.GetSha256() != d.SHA256 {
		return fmt.Errorf("bootstrap bundle does not match expected revision and digest")
	}
	return nil
}

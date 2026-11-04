package config

import (
	"os"
	"path/filepath"
	"testing"

	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
)

func TestMaterializeBootstrapExpandsCredentialPlaceholder(t *testing.T) {
	bundle := &runtimev1.BootstrapServiceGetBootstrapConfigResponse{ConfigJson: []byte(`{"headers":{"Authorization":"__KAGENT_ENV[KAGENT_CREDENTIAL_TEST]__"}}`), AgentCardJson: []byte(`{"name":"agent"}`)}
	t.Setenv("KAGENT_CREDENTIAL_TEST", "Bearer secret")
	dir := t.TempDir()
	if err := materializeBootstrapFiles(dir, bundle); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"headers":{"Authorization":"Bearer secret"}}` {
		t.Fatalf("config = %s", got)
	}
}

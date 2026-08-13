package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeFromEnvExpandsCredentialPlaceholder(t *testing.T) {
	t.Setenv(envAgentConfigJSON, `{"headers":{"Authorization":"__KAGENT_ENV[KAGENT_CREDENTIAL_TEST]__"}}`)
	t.Setenv("KAGENT_CREDENTIAL_TEST", "Bearer secret")
	dir := t.TempDir()
	if err := MaterializeFromEnv(dir); err != nil {
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

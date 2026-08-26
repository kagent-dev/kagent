package adapter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/harness/claude/config"
)

func TestNewMaterializesDurableDirectories(t *testing.T) {
	durableDir := filepath.Join(t.TempDir(), "data")
	ephemeralDir := filepath.Join(t.TempDir(), "credentials")
	workspace := filepath.Join(durableDir, "workspace")
	runner, err := New(Input{
		ConfigJSON: []byte(`{"version":2,"claude_executable":"claude","expected_claude_version":"2.1.217","strict_version":true,"max_event_bytes":100,"max_stderr_bytes":100,"interrupt_grace_millis":100}`),
		Workspace:  workspace,
		DurableDir: durableDir, EphemeralDir: ephemeralDir,
		Environment: []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/wrong", "DISABLE_AUTOUPDATER=0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner == nil {
		t.Fatal("New() returned a nil runner")
	}
	for _, path := range []string{workspace, filepath.Join(durableDir, "claude"), filepath.Join(durableDir, "claude", "skills")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("%s permissions = %o, want 700", path, info.Mode().Perm())
		}
	}
}

func TestNewRejectsInvalidInput(t *testing.T) {
	input := Input{ConfigJSON: []byte(`{}`), Workspace: "relative", DurableDir: "relative", EphemeralDir: "relative"}
	if _, err := New(input); err == nil {
		t.Fatal("New() accepted invalid input")
	}
}

func TestMaterializeGoogleCredentials(t *testing.T) {
	dir := t.TempDir()
	raw := `{"type":"service_account","project_id":"test"}`
	environment, err := materializeGoogleCredentials([]string{"A=1", config.GoogleCredentialsJSONEnvName + "=" + raw}, dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "google-credentials.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != raw {
		t.Fatalf("credentials = %q", contents)
	}
	if len(environment) != 2 || environment[0] != "A=1" || environment[1] != googleCredsEnv+"="+path {
		t.Fatalf("environment = %v", environment)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential permissions = %v, %v", info, err)
	}
}

func TestSetEnvironmentOverridesExistingValue(t *testing.T) {
	got := setEnvironment([]string{"A=1", "A=2", "B=3"}, "A", "4")
	want := []string{"B=3", "A=4"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("setEnvironment() = %v, want %v", got, want)
	}
}

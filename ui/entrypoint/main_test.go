package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMatchesInitScript runs the shell entrypoint this program replaced against the
// same environments and checks both write identical files and warnings.
func TestMatchesInitScript(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}
	script, err := os.ReadFile("testdata/init.sh")
	if err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile("../index.html")
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string][]string{
		"defaults":                  nil,
		"empty values use defaults": {"KAGENT_UI_API_BASE_URL=", "KAGENT_UI_STREAM_TIMEOUT_MS=", "KAGENT_UI_BASE_PATH="},
		"all set": {
			"KAGENT_UI_API_BASE_URL=https://x.test/api?a=\"b\"&c=<d>\\e",
			"KAGENT_UI_SSO_REDIRECT_PATH=/sso",
			"KAGENT_UI_STREAM_TIMEOUT_MS=0042",
			"KAGENT_UI_ENABLE_MOCK=true",
			"KAGENT_UI_BASE_PATH=/ui/",
		},
		"nested base path":       {"KAGENT_UI_BASE_PATH=/a/b.c~d-e_f"},
		"reserved base path":     {"KAGENT_UI_BASE_PATH=/api/x"},
		"dot segment base path":  {"KAGENT_UI_BASE_PATH=/a/../b"},
		"relative base path":     {"KAGENT_UI_BASE_PATH=ui"},
		"base path with a space": {"KAGENT_UI_BASE_PATH=/u i"},
		"bad timeout":            {"KAGENT_UI_STREAM_TIMEOUT_MS=-1"},
		"extensions": {
			"KAGENT_UI_EXTENSION_B=two=equals=",
			"KAGENT_UI_EXTENSION_A1=digit sorts first",
			"KAGENT_UI_EXTENSION_A=</script>\"\\",
			"KAGENT_UI_EXTENSION_EMPTY=",
			"KAGENT_UI_EXTENSION_LINES=first\nsecond",
			"KAGENT_UI_EXTENSIONLESS=ignored",
		},
	}
	for name, vars := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "html"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "html/index.html"), html, 0o644); err != nil {
				t.Fatal(err)
			}
			patched := strings.NewReplacer(
				"/tmp/", root+"/tmp/",
				"/usr/share/nginx/html/", root+"/html/",
				`exec nginx -g "daemon off;"`, "exit 0",
			).Replace(string(script))
			var stderr bytes.Buffer
			cmd := exec.Command(bash, "-c", patched)
			cmd.Env = append([]string{"PATH=/usr/bin:/bin"}, vars...)
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("init.sh: %v: %s", err, stderr.String())
			}
			wantConfig, _ := os.ReadFile(filepath.Join(root, "tmp/kagent/env-config.js"))
			wantHTML, _ := os.ReadFile(filepath.Join(root, "tmp/kagent/index.html"))
			wantStderr := strings.ReplaceAll(stderr.String(), "init.sh:", "entrypoint:")

			r, w, _ := os.Pipe()
			saved := os.Stderr
			os.Stderr = w
			gotConfig, basePath := render(cmd.Env)
			gotHTML := setBaseHref(html, basePath)
			os.Stderr = saved
			w.Close()
			var gotStderr bytes.Buffer
			gotStderr.ReadFrom(r)

			if gotConfig != string(wantConfig) {
				t.Errorf("env-config.js differs\n got: %s\nwant: %s", gotConfig, wantConfig)
			}
			if !bytes.Equal(gotHTML, wantHTML) {
				t.Errorf("index.html differs\n got: %s\nwant: %s", gotHTML, wantHTML)
			}
			if gotStderr.String() != wantStderr {
				t.Errorf("stderr differs\n got: %q\nwant: %q", gotStderr.String(), wantStderr)
			}
		})
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMatchesInitScript runs the shell entrypoint this program replaced against the
// same environments and checks both write identical files and warnings. Multi-line
// values are left out: init.sh kept only their first line, which TestRenderLineBreaks covers.
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

// TestRenderLineBreaks checks that extension values arrive whole, and that no value can
// put a raw line break into a string literal and take every other setting down with it.
func TestRenderLineBreaks(t *testing.T) {
	cases := map[string]struct {
		env  []string
		want map[string]string
	}{
		"multi-line value": {
			env:  []string{"KAGENT_UI_EXTENSION_CSS=a { color: red; }\nb { color: blue; }"},
			want: map[string]string{"KAGENT_UI_EXTENSION_CSS": "a { color: red; }\nb { color: blue; }"},
		},
		"CRLF value": {
			env:  []string{"KAGENT_UI_EXTENSION_CSS=a{}\r\nb{}", "KAGENT_UI_EXTENSION_OTHER=kept"},
			want: map[string]string{"KAGENT_UI_EXTENSION_CSS": "a{}\r\nb{}", "KAGENT_UI_EXTENSION_OTHER": "kept"},
		},
		"line separators": {
			env:  []string{"KAGENT_UI_EXTENSION_MSG=a\u2028b\u2029c"},
			want: map[string]string{"KAGENT_UI_EXTENSION_MSG": "a\u2028b\u2029c"},
		},
		"continuation line is not a key": {
			env:  []string{"KAGENT_UI_EXTENSION_MSG=hello\nKAGENT_UI_EXTENSION_ADMIN=true"},
			want: map[string]string{"KAGENT_UI_EXTENSION_MSG": "hello\nKAGENT_UI_EXTENSION_ADMIN=true"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			config, _ := render(tc.env)
			object, ok := strings.CutPrefix(config, "window.environmentVariables = ")
			object, ok2 := strings.CutSuffix(object, ";\n")
			if !ok || !ok2 {
				t.Fatalf("unexpected env-config.js shape: %q", config)
			}
			// A JS string literal cannot hold a raw CR, LF, U+2028 or U+2029 on every engine, so none may appear inside one.
			for _, line := range strings.Split(object, "\n") {
				if strings.ContainsAny(line, "\r\u2028\u2029") {
					t.Errorf("raw line terminator in %q", line)
				}
			}
			var got map[string]string
			if err := json.Unmarshal([]byte(object), &got); err != nil {
				t.Fatalf("env-config.js object is not valid JSON: %v\n%s", err, object)
			}
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("%s = %q, want %q", key, got[key], want)
				}
			}
			if _, ok := got["KAGENT_UI_EXTENSION_ADMIN"]; ok {
				t.Errorf("a continuation line became the key KAGENT_UI_EXTENSION_ADMIN")
			}
		})
	}
}

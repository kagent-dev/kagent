package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// capture returns what fn writes to stderr.
func capture(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	fn()
	os.Stderr = saved
	w.Close()
	var b bytes.Buffer
	b.ReadFrom(r)
	return b.String()
}

func TestRender(t *testing.T) {
	cases := map[string]struct {
		env      []string
		config   string
		basePath string
		warning  string
	}{
		"defaults": {
			config: `window.environmentVariables = {
  "API_BASE_URL": "/api",
  "SSO_REDIRECT_PATH": "/oauth2/start",
  "STREAM_TIMEOUT_MS": "1800000",
  "BASE_PATH": "",
  "ENABLE_MOCK_UI": "false"
};
`,
		},
		"empty values use defaults": {
			env: []string{"KAGENT_UI_API_BASE_URL=", "KAGENT_UI_STREAM_TIMEOUT_MS=", "KAGENT_UI_BASE_PATH="},
			config: `window.environmentVariables = {
  "API_BASE_URL": "/api",
  "SSO_REDIRECT_PATH": "/oauth2/start",
  "STREAM_TIMEOUT_MS": "1800000",
  "BASE_PATH": "",
  "ENABLE_MOCK_UI": "false"
};
`,
		},
		"all set and escaped": {
			env: []string{
				"KAGENT_UI_API_BASE_URL=https://x.test/api?a=\"b\"&c=<d>\\e",
				"KAGENT_UI_SSO_REDIRECT_PATH=/sso",
				"KAGENT_UI_STREAM_TIMEOUT_MS=0042",
				"KAGENT_UI_ENABLE_MOCK=true",
				"KAGENT_UI_BASE_PATH=/ui/",
			},
			config: `window.environmentVariables = {
  "API_BASE_URL": "https://x.test/api?a=\"b\"&c=\u003cd>\\e",
  "SSO_REDIRECT_PATH": "/sso",
  "STREAM_TIMEOUT_MS": "0042",
  "BASE_PATH": "/ui",
  "ENABLE_MOCK_UI": "true"
};
`,
			basePath: "/ui",
		},
		"extensions are sorted, escaped and cut to one line": {
			env: []string{
				"KAGENT_UI_EXTENSION_B=two=equals=",
				"KAGENT_UI_EXTENSION_A1=digit sorts first",
				"KAGENT_UI_EXTENSION_A=</script>\"\\",
				"KAGENT_UI_EXTENSION_EMPTY=",
				"KAGENT_UI_EXTENSION_LINES=first\nsecond",
				"KAGENT_UI_EXTENSIONLESS=ignored",
			},
			config: `window.environmentVariables = {
  "API_BASE_URL": "/api",
  "SSO_REDIRECT_PATH": "/oauth2/start",
  "STREAM_TIMEOUT_MS": "1800000",
  "KAGENT_UI_EXTENSION_A1": "digit sorts first",
  "KAGENT_UI_EXTENSION_A": "\u003c/script>\"\\",
  "KAGENT_UI_EXTENSION_B": "two=equals=",
  "KAGENT_UI_EXTENSION_EMPTY": "",
  "KAGENT_UI_EXTENSION_LINES": "first",
  "BASE_PATH": "",
  "ENABLE_MOCK_UI": "false"
};
`,
		},
		"bad timeout falls back": {
			env:     []string{"KAGENT_UI_STREAM_TIMEOUT_MS=-1"},
			warning: "entrypoint: KAGENT_UI_STREAM_TIMEOUT_MS='-1' is not a number; using 1800000\n",
		},
		"nested base path": {env: []string{"KAGENT_UI_BASE_PATH=/a/b.c~d-e_f"}, basePath: "/a/b.c~d-e_f"},
	}
	for _, bad := range []string{"/api/x", "/a/../b", "ui", "/u i"} {
		cases["rejects base path "+bad] = struct {
			env      []string
			config   string
			basePath string
			warning  string
		}{
			env:     []string{"KAGENT_UI_BASE_PATH=" + bad},
			warning: "entrypoint: KAGENT_UI_BASE_PATH='" + bad + "' is not a path like /ui, or starts with a path the UI serves; serving at the root\n",
		}
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var config, basePath string
			warning := capture(t, func() { config, basePath = render(tc.env) })
			if tc.config != "" && config != tc.config {
				t.Errorf("env-config.js\n got: %q\nwant: %q", config, tc.config)
			}
			if basePath != tc.basePath || warning != tc.warning {
				t.Errorf("basePath %q warning %q, want %q %q", basePath, warning, tc.basePath, tc.warning)
			}
		})
	}
}

func TestSetBaseHref(t *testing.T) {
	html, err := os.ReadFile("../index.html")
	if err != nil {
		t.Fatal(err)
	}
	for basePath, want := range map[string]string{"": `<base href="/"`, "/ui": `<base href="/ui/"`} {
		var got []byte
		if warning := capture(t, func() { got = setBaseHref(html, basePath) }); warning != "" || !strings.Contains(string(got), want) {
			t.Errorf("%q: warning %q, missing %s", basePath, warning, want)
		}
	}
	if warning := capture(t, func() { setBaseHref([]byte("<html></html>"), "/ui") }); warning == "" {
		t.Error("no warning when <base href> is missing")
	}
}

// Command entrypoint is the UI image's entrypoint: it renders the browser settings
// from the environment into env-config.js, sets <base href>, then execs nginx.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"syscall"
)

// Keep in sync with the ui.basePath check in helm/kagent/templates/ui-configmap.yaml.
var (
	basePathRe     = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)*$`)
	dotSegmentRe   = regexp.MustCompile(`(^|/)\.\.?(/|$)`)
	reservedPathRe = regexp.MustCompile(`^/(api|a2a|assets|health|env-config\.js|index\.html|mockServiceWorker\.js)(/|$)`)
	digitsRe       = regexp.MustCompile(`^[0-9]+$`)
)

func main() {
	// /tmp is an empty volume under a read-only root filesystem, so nginx's temp dirs are made on every start.
	for _, dir := range []string{"client_temp", "proxy_temp", "fastcgi_temp", "uwsgi_temp", "scgi_temp"} {
		must(os.MkdirAll("/tmp/nginx/"+dir, 0o755))
	}
	must(os.MkdirAll("/tmp/kagent", 0o755))

	config, basePath := render(os.Environ())
	must(os.WriteFile("/tmp/kagent/env-config.js", []byte(config), 0o644))

	html, err := os.ReadFile("/usr/share/nginx/html/index.html")
	must(err)
	html = setBaseHref(html, basePath)
	must(os.WriteFile("/tmp/kagent/index.html", html, 0o644))

	nginx, err := exec.LookPath("nginx")
	must(err)
	must(syscall.Exec(nginx, []string{"nginx", "-g", "daemon off;"}, os.Environ()))
}

// render returns env-config.js for the given environment, and the validated base path.
func render(environ []string) (string, string) {
	env := map[string]string{}
	var extensions []string
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		env[name] = value
		if strings.HasPrefix(kv, "KAGENT_UI_EXTENSION_") {
			// Sorted as whole NAME=value lines, first line of the value only, to match `env | sort`.
			first, _, _ := strings.Cut(kv, "\n")
			extensions = append(extensions, first)
		}
	}
	get := func(name, fallback string) string {
		if v := env[name]; v != "" {
			return v
		}
		return fallback
	}

	basePath := strings.TrimSuffix(get("KAGENT_UI_BASE_PATH", ""), "/")
	if !basePathRe.MatchString(basePath) || dotSegmentRe.MatchString(basePath) || reservedPathRe.MatchString(basePath) {
		fmt.Fprintf(os.Stderr, "entrypoint: KAGENT_UI_BASE_PATH='%s' is not a path like /ui, or starts with a path nginx serves; serving at the root\n", basePath)
		basePath = ""
	}
	// A non-numeric timeout would abort every chat stream at once, so fall back instead.
	timeout := get("KAGENT_UI_STREAM_TIMEOUT_MS", "1800000")
	if !digitsRe.MatchString(timeout) {
		fmt.Fprintf(os.Stderr, "entrypoint: KAGENT_UI_STREAM_TIMEOUT_MS='%s' is not a number; using 1800000\n", timeout)
		timeout = "1800000"
	}

	// Extension settings pass through by prefix, so this image never lists another product's keys.
	slices.Sort(extensions)
	var b strings.Builder
	b.WriteString("window.environmentVariables = {\n")
	fmt.Fprintf(&b, "  \"API_BASE_URL\": \"%s\",\n", jsonEscape(get("KAGENT_UI_API_BASE_URL", "/api")))
	fmt.Fprintf(&b, "  \"SSO_REDIRECT_PATH\": \"%s\",\n", jsonEscape(get("KAGENT_UI_SSO_REDIRECT_PATH", "/oauth2/start")))
	fmt.Fprintf(&b, "  \"STREAM_TIMEOUT_MS\": \"%s\",\n", timeout)
	for _, kv := range extensions {
		name, value, _ := strings.Cut(kv, "=")
		fmt.Fprintf(&b, "  \"%s\": \"%s\",\n", jsonEscape(name), jsonEscape(value))
	}
	fmt.Fprintf(&b, "  \"BASE_PATH\": \"%s\",\n", basePath)
	fmt.Fprintf(&b, "  \"ENABLE_MOCK_UI\": \"%s\"\n", jsonEscape(get("KAGENT_UI_ENABLE_MOCK", "false")))
	b.WriteString("};\n")
	return b.String(), basePath
}

// jsonEscape makes a value safe inside a JSON string in a <script> element: `<` is escaped so markup cannot close it.
func jsonEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, `<`, `\u003c`).Replace(s)
}

// setBaseHref points <base href> at the base path, so deep links load assets under the sub-path too.
func setBaseHref(html []byte, basePath string) []byte {
	lines := strings.Split(string(html), "\n")
	for i, line := range lines {
		lines[i] = strings.Replace(line, `<base href="/"`, `<base href="`+basePath+`/"`, 1)
	}
	out := strings.Join(lines, "\n")
	if !strings.Contains(out, `<base href="`+basePath+`/"`) {
		fmt.Fprintf(os.Stderr, "entrypoint: could not set <base href> to %s/\n", basePath)
	}
	return []byte(out)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "entrypoint:", err)
		os.Exit(1)
	}
}

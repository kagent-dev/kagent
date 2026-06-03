package egress

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeClient(existing ...client.Object) client.Client {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = v1alpha2.AddToScheme(s)
	return fake.NewClientBuilder().WithScheme(s).WithObjects(existing...).Build()
}

func newRMS(name, namespace, url string) *v1alpha2.RemoteMCPServer {
	return &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       v1alpha2.RemoteMCPServerSpec{URL: url},
	}
}

func newAgentWithRMSTools(namespace string, rmsNames ...string) *v1alpha2.Agent {
	tools := make([]*v1alpha2.Tool, 0, len(rmsNames))
	for _, n := range rmsNames {
		tools = append(tools, &v1alpha2.Tool{
			McpServer: &v1alpha2.McpServerTool{
				TypedReference: v1alpha2.TypedReference{
					ApiGroup: "kagent.dev",
					Kind:     "RemoteMCPServer",
					Name:     n,
				},
			},
		})
	}
	return &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-agent", Namespace: namespace},
		Spec: v1alpha2.AgentSpec{
			Declarative: &v1alpha2.DeclarativeAgentSpec{Tools: tools},
		},
	}
}

func TestRewriteHTTPSForEgress(t *testing.T) {
	// rmsHosts mirrors the map collectRMSHosts would build: each entry's key is
	// an RMS's spec.url string (verbatim — same string the translator emits as
	// the tool URL), and the value is the RMS's effective tls-aware host:port.
	// For the last entry the spec.url is scheme-less and port-less but spec.tls
	// is set, so the effective is :443.
	allRMSHosts := map[string]string{
		"https://api.githubcopilot.com/mcp/":     "api.githubcopilot.com:443",
		"https://host.docker.internal:13443/mcp": "host.docker.internal:13443",
		"https://api.example.com/v1?token=x":     "api.example.com:443",
		"https://[2001:db8::1]:8443/v1":          "[2001:db8::1]:8443",
		"https://[2001:db8::1]/v1":               "[2001:db8::1]:443",
		"http://kagent-tools.kagent:8084/mcp":    "kagent-tools.kagent:8084",
		"host.docker.internal:13443/mcp":         "host.docker.internal:13443",
		"tls-svc.example.com/mcp":                "tls-svc.example.com:443",
	}
	cases := []struct {
		name     string
		in       string
		rmsHosts map[string]string
		want     string
	}{
		{"https matched and rewritten to effective 443", "https://api.githubcopilot.com/mcp/", allRMSHosts, "http://api.githubcopilot.com:443/mcp/"},
		{"https with explicit port preserved", "https://host.docker.internal:13443/mcp", allRMSHosts, "http://host.docker.internal:13443/mcp"},
		{"http matched stays plaintext on its port", "http://kagent-tools.kagent:8084/mcp", allRMSHosts, "http://kagent-tools.kagent:8084/mcp"},
		{"query string preserved", "https://api.example.com/v1?token=x", allRMSHosts, "http://api.example.com:443/v1?token=x"},
		{"unrecognized url left alone", "not-a-url", allRMSHosts, "not-a-url"},
		{"ipv6 literal preserves brackets", "https://[2001:db8::1]:8443/v1", allRMSHosts, "http://[2001:db8::1]:8443/v1"},
		{"ipv6 literal default port", "https://[2001:db8::1]/v1", allRMSHosts, "http://[2001:db8::1]:443/v1"},
		{"url not in rms map left alone", "https://stray.example.com/mcp", allRMSHosts, "https://stray.example.com/mcp"},
		{"empty rms set leaves https untouched", "https://api.example.com/v1", map[string]string{}, "https://api.example.com/v1"},
		{"scheme-less matched with explicit port", "host.docker.internal:13443/mcp", allRMSHosts, "http://host.docker.internal:13443/mcp"},
		{"scheme-less port-less rewrites to effective tls port", "tls-svc.example.com/mcp", allRMSHosts, "http://tls-svc.example.com:443/mcp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, rewriteHTTPSForEgress(c.in, c.rmsHosts))
		})
	}
}

func TestRewriteDialURL(t *testing.T) {
	rmsWith := func(url string, tls *v1alpha2.TLSConfig) *v1alpha2.RemoteMCPServer {
		return &v1alpha2.RemoteMCPServer{Spec: v1alpha2.RemoteMCPServerSpec{URL: url, TLS: tls}}
	}
	rms := func(u string) *v1alpha2.RemoteMCPServer { return rmsWith(u, nil) }

	cases := []struct {
		name string
		rms  *v1alpha2.RemoteMCPServer
		want string
	}{
		{"https without port defaults to 443", rms("https://upstream.example.com/mcp"), "http://upstream.example.com:443/mcp"},
		{"https with explicit port preserved", rms("https://upstream.example.com:8443/mcp"), "http://upstream.example.com:8443/mcp"},
		{"query string preserved", rms("https://upstream.example.com/v1?token=x"), "http://upstream.example.com:443/v1?token=x"},
		{"http with explicit port preserved", rms("http://svc.ns:8080/mcp"), "http://svc.ns:8080/mcp"},
		// Scheme-less is rewritten just like the agent path.
		{"scheme-less with explicit port rewritten", rms("host.docker.internal:13443/mcp"), "http://host.docker.internal:13443/mcp"},
		{"scheme-less no port no tls defaults to 80", rms("svc.ns/mcp"), "http://svc.ns:80/mcp"},
		// Per CRD-validated contract, spec.tls != nil signals TLS opt-in (even
		// the empty struct {}); the previously-divergent shape (scheme-less,
		// port-less, TLS-backed) resolves to :443 on both paths. Only the
		// http://+non-nil-tls combo is admission-rejected; the http://+tls
		// case below is kept as defensive coverage if a webhook is bypassed.
		{"scheme-less no port + empty tls uses effective 443", rmsWith("tls-svc.example.com/mcp", &v1alpha2.TLSConfig{}), "http://tls-svc.example.com:443/mcp"},
		{"scheme-less no port + non-empty tls uses effective 443", rmsWith("tls-svc.example.com/mcp", &v1alpha2.TLSConfig{CACertSecretRef: "ca", CACertSecretKey: "ca.crt"}), "http://tls-svc.example.com:443/mcp"},
		{"http no port + tls (admission would reject) still upgrades", rmsWith("http://tls-svc.example.com/mcp", &v1alpha2.TLSConfig{}), "http://tls-svc.example.com:443/mcp"},
		{"ipv6 default port", rms("https://[2001:db8::1]/v1"), "http://[2001:db8::1]:443/v1"},
		{"non-http scheme passes through", rms("ftp://example.com/x"), "ftp://example.com/x"},
		{"unparseable passes through", rms("://"), "://"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, RewriteDialURL(c.rms))
		})
	}
}

func TestNormalizedHostPort(t *testing.T) {
	rmsWith := func(url string, tls *v1alpha2.TLSConfig) *v1alpha2.RemoteMCPServer {
		return &v1alpha2.RemoteMCPServer{Spec: v1alpha2.RemoteMCPServerSpec{URL: url, TLS: tls}}
	}
	rms := func(u string) *v1alpha2.RemoteMCPServer { return rmsWith(u, nil) }

	cases := []struct {
		name string
		rms  *v1alpha2.RemoteMCPServer
		want string
	}{
		{"https no-tls default port", rms("https://api.example.com/mcp"), "api.example.com:443"},
		{"http no-tls default port", rms("http://svc.ns:8080/mcp"), "svc.ns:8080"},
		{"https explicit port preserved", rms("https://host.docker.internal:13443/mcp"), "host.docker.internal:13443"},
		{"ipv6 https default port", rms("https://[2001:db8::1]/v1"), "[2001:db8::1]:443"},
		{"scheme-less no-tls defaults to 80", rms("api.example.com/mcp"), "api.example.com:80"},
		// spec.tls != nil is the TLS opt-in signal (CRD-validated contract);
		// empty struct counts the same as a populated one for the runtime.
		{"scheme-less + empty tls defaults to 443", rmsWith("api.example.com/mcp", &v1alpha2.TLSConfig{}), "api.example.com:443"},
		{"scheme-less + non-empty tls defaults to 443", rmsWith("api.example.com/mcp", &v1alpha2.TLSConfig{CACertSecretRef: "ca", CACertSecretKey: "ca.crt"}), "api.example.com:443"},
		{"scheme-less with explicit port + tls", rmsWith("host.docker.internal:13443/mcp", &v1alpha2.TLSConfig{}), "host.docker.internal:13443"},
		{"http + tls upgrades port default to 443 (admission rejects this combo)", rmsWith("http://api.example.com/v1", &v1alpha2.TLSConfig{}), "api.example.com:443"},
		{"empty input", rms(""), ""},
		{"non-http scheme", rms("ftp://example.com/x"), ""},
		{"malformed", rms("://"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, normalizedHostPort(c.rms))
		})
	}
}

// httpToolURLs/sseToolURLs read back the tool URLs the rewrite mutated in place.
func httpToolURL(cfg *adk.AgentConfig) string { return cfg.HttpTools[0].Params.Url }
func sseToolURL(cfg *adk.AgentConfig) string  { return cfg.SseTools[0].Params.Url }

func newConfig(httpURL, sseURL string) *adk.AgentConfig {
	return &adk.AgentConfig{
		Model: &adk.OpenAI{BaseModel: adk.BaseModel{Type: adk.ModelTypeOpenAI, Model: "gpt-4"}},
		HttpTools: []adk.HttpMcpServerConfig{
			{Params: adk.StreamableHTTPConnectionParams{Url: httpURL}},
		},
		SseTools: []adk.SseMcpServerConfig{
			{Params: adk.SseConnectionParams{Url: sseURL}},
		},
	}
}

func TestRewriteConfigForAgent(t *testing.T) {
	const ns = "egress-test"
	ctx := context.Background()

	t.Run("rewrites RMS-backed http and SSE tool URLs", func(t *testing.T) {
		httpRMS := newRMS("github-copilot", ns, "https://api.githubcopilot.com/mcp/")
		sseRMS := newRMS("sse-svc", ns, "https://sse.example.com:8443/events")
		agent := newAgentWithRMSTools(ns, "github-copilot", "sse-svc")

		cfg := newConfig("https://api.githubcopilot.com/mcp/", "https://sse.example.com:8443/events")
		assert.NoError(t, RewriteConfigForAgent(ctx, newFakeClient(httpRMS, sseRMS), agent, cfg))

		assert.Equal(t, "http://api.githubcopilot.com:443/mcp/", httpToolURL(cfg))
		assert.Equal(t, "http://sse.example.com:8443/events", sseToolURL(cfg))
	})

	t.Run("https URL with no matching RMS ref is left untouched", func(t *testing.T) {
		// Models the globalProxyURL=https edge case: the URL is https but no
		// RMS spec.URL matches its host:port, so it must NOT be rewritten.
		rms := newRMS("github-copilot", ns, "https://api.githubcopilot.com/mcp/")
		agent := newAgentWithRMSTools(ns, "github-copilot")

		cfg := newConfig("https://internal-proxy.example.com/v1/foo", "https://api.githubcopilot.com/mcp/")
		assert.NoError(t, RewriteConfigForAgent(ctx, newFakeClient(rms), agent, cfg))

		assert.Equal(t, "https://internal-proxy.example.com/v1/foo", httpToolURL(cfg), "non-RMS URL must not be rewritten")
		assert.Equal(t, "http://api.githubcopilot.com:443/mcp/", sseToolURL(cfg), "RMS-matched URL must be rewritten")
	})

	t.Run("http URLs left alone (plaintext in-cluster RMSs)", func(t *testing.T) {
		rms1 := newRMS("kagent-tools", ns, "http://kagent-tools.kagent:8084/mcp")
		rms2 := newRMS("kagent-grafana-mcp", ns, "http://kagent-grafana-mcp.kagent:8000/mcp")
		agent := newAgentWithRMSTools(ns, "kagent-tools", "kagent-grafana-mcp")

		cfg := newConfig("http://kagent-tools.kagent:8084/mcp", "http://kagent-grafana-mcp.kagent:8000/mcp")
		assert.NoError(t, RewriteConfigForAgent(ctx, newFakeClient(rms1, rms2), agent, cfg))

		assert.Equal(t, "http://kagent-tools.kagent:8084/mcp", httpToolURL(cfg))
		assert.Equal(t, "http://kagent-grafana-mcp.kagent:8000/mcp", sseToolURL(cfg))
	})

	t.Run("agent with no RMS tool refs is a no-op (skill-only / MCPServer-only)", func(t *testing.T) {
		agent := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "demo-agent", Namespace: ns},
			Spec:       v1alpha2.AgentSpec{Declarative: &v1alpha2.DeclarativeAgentSpec{}},
		}
		cfg := newConfig("https://api.githubcopilot.com/mcp/", "https://sse.example.com:8443/events")
		assert.NoError(t, RewriteConfigForAgent(ctx, newFakeClient(), agent, cfg))

		assert.Equal(t, "https://api.githubcopilot.com/mcp/", httpToolURL(cfg))
		assert.Equal(t, "https://sse.example.com:8443/events", sseToolURL(cfg))
	})

	t.Run("missing RMS (NotFound) is silently skipped, others still rewrite", func(t *testing.T) {
		rms := newRMS("github-copilot", ns, "https://api.githubcopilot.com/mcp/")
		agent := newAgentWithRMSTools(ns, "github-copilot", "missing-rms")

		cfg := newConfig("https://api.githubcopilot.com/mcp/", "https://api.githubcopilot.com/mcp/")
		assert.NoError(t, RewriteConfigForAgent(ctx, newFakeClient(rms), agent, cfg))

		assert.Equal(t, "http://api.githubcopilot.com:443/mcp/", httpToolURL(cfg))
		assert.Equal(t, "http://api.githubcopilot.com:443/mcp/", sseToolURL(cfg))
	})

	t.Run("scheme-less + TLS RMS rewrites tool URLs to the effective 443", func(t *testing.T) {
		// spec.url is scheme-less and port-less but spec.tls is set (any
		// non-nil pointer, including the empty struct, is the TLS opt-in
		// signal per the CRD-validated contract), so the effective upstream
		// port is 443. The agent's tool URLs and the controller's dial must
		// both resolve to http://host:443.
		rms := &v1alpha2.RemoteMCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "tls-svc", Namespace: ns},
			Spec:       v1alpha2.RemoteMCPServerSpec{URL: "tls-svc.example.com/mcp", TLS: &v1alpha2.TLSConfig{}},
		}
		agent := newAgentWithRMSTools(ns, "tls-svc")

		cfg := newConfig("tls-svc.example.com/mcp", "tls-svc.example.com/mcp")
		assert.NoError(t, RewriteConfigForAgent(ctx, newFakeClient(rms), agent, cfg))

		assert.Equal(t, "http://tls-svc.example.com:443/mcp", httpToolURL(cfg))
		assert.Equal(t, "http://tls-svc.example.com:443/mcp", sseToolURL(cfg))
		// The dial path resolves the same RMS to the identical endpoint.
		assert.Equal(t, "http://tls-svc.example.com:443/mcp", RewriteDialURL(rms))
	})

	t.Run("nil config is a no-op", func(t *testing.T) {
		agent := newAgentWithRMSTools(ns, "github-copilot")
		assert.NoError(t, RewriteConfigForAgent(ctx, newFakeClient(), agent, nil))
	})
}

func TestParseRemoteMCPServerURL(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantHost string
		wantPort string
		wantPath string
		wantErr  bool
	}{
		{"https with path", "https://api.example.com/mcp", "api.example.com", "", "/mcp", false},
		{"explicit port preserved", "https://svc:8443/mcp", "svc", "8443", "/mcp", false},
		{"scheme-less explicit port", "svc:13443/mcp", "svc", "13443", "/mcp", false},
		{"scheme-less no port", "svc/mcp", "svc", "", "/mcp", false},
		{"query preserved", "https://api/v1?token=x", "api", "", "/v1", false},
		{"ipv6 literal", "https://[2001:db8::1]:8443/v1", "2001:db8::1", "8443", "/v1", false},
		{"empty input errors", "", "", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, err := ParseRemoteMCPServerURL(c.in)
			if c.wantErr {
				assert.Error(t, err)
				return
			}
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, c.wantHost, u.Hostname())
			assert.Equal(t, c.wantPort, u.Port())
			assert.Equal(t, c.wantPath, u.Path)
		})
	}
}

func TestEffectiveScheme(t *testing.T) {
	rmsWith := func(url string, tls *v1alpha2.TLSConfig) *v1alpha2.RemoteMCPServer {
		return &v1alpha2.RemoteMCPServer{Spec: v1alpha2.RemoteMCPServerSpec{URL: url, TLS: tls}}
	}
	rms := func(u string) *v1alpha2.RemoteMCPServer { return rmsWith(u, nil) }
	nonEmptyTLS := &v1alpha2.TLSConfig{CACertSecretRef: "ca", CACertSecretKey: "ca.crt"}

	cases := []struct {
		name string
		rms  *v1alpha2.RemoteMCPServer
		want string
	}{
		{"https url no tls", rms("https://api.example.com/mcp"), "https"},
		{"http url no tls", rms("http://svc.ns/mcp"), "http"},
		{"scheme-less no tls", rms("svc.ns/mcp"), "http"},
		{"non-empty tls + http url", rmsWith("http://svc/mcp", nonEmptyTLS), "https"},
		{"non-empty tls + scheme-less", rmsWith("svc/mcp", nonEmptyTLS), "https"},
		{"non-empty tls + DisableVerify only", rmsWith("svc/mcp", &v1alpha2.TLSConfig{DisableVerify: true}), "https"},
		// Per CRD-validated contract, spec.tls != nil ⇒ TLS opt-in, even when
		// the struct has no fields set. Only http://+non-nil-tls is admission-
		// rejected; the http://+tls case below is kept as defensive coverage if
		// a webhook is bypassed. The runtime's safer answer is "https" whenever
		// either signal expresses TLS intent.
		{"empty tls struct + scheme-less → https (opt-in)", rmsWith("svc/mcp", &v1alpha2.TLSConfig{}), "https"},
		{"empty tls struct + http → https (admission rejects, runtime defaults safer)", rmsWith("http://svc/mcp", &v1alpha2.TLSConfig{}), "https"},
		{"empty tls struct + https → https", rmsWith("https://svc/mcp", &v1alpha2.TLSConfig{}), "https"},
		{"unparseable url no tls falls back to http", rms("://"), "http"},
		{"non-empty tls overrides parse failure", rmsWith("://", nonEmptyTLS), "https"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, EffectiveScheme(c.rms))
		})
	}
}

func TestEffectivePort(t *testing.T) {
	rmsWith := func(url string, tls *v1alpha2.TLSConfig) *v1alpha2.RemoteMCPServer {
		return &v1alpha2.RemoteMCPServer{Spec: v1alpha2.RemoteMCPServerSpec{URL: url, TLS: tls}}
	}
	rms := func(u string) *v1alpha2.RemoteMCPServer { return rmsWith(u, nil) }
	nonEmptyTLS := &v1alpha2.TLSConfig{CACertSecretRef: "ca", CACertSecretKey: "ca.crt"}

	cases := []struct {
		name string
		rms  *v1alpha2.RemoteMCPServer
		want int32
	}{
		{"https no port defaults to 443", rms("https://svc/mcp"), 443},
		{"http no port defaults to 80", rms("http://svc/mcp"), 80},
		{"https explicit port preserved", rms("https://svc:8443/mcp"), 8443},
		{"http explicit port preserved", rms("http://svc:8080/mcp"), 8080},
		{"scheme-less explicit port", rms("svc:13443/mcp"), 13443},
		{"scheme-less no port no tls defaults to 80", rms("svc/mcp"), 80},
		{"non-empty tls + scheme-less defaults to 443", rmsWith("svc/mcp", nonEmptyTLS), 443},
		{"non-empty tls + http no port defaults to 443", rmsWith("http://svc/mcp", nonEmptyTLS), 443},
		// Per CRD-validated contract: empty struct {} counts as TLS opt-in too.
		{"empty tls struct + scheme-less defaults to 443", rmsWith("svc/mcp", &v1alpha2.TLSConfig{}), 443},
		{"empty tls struct + http defaults to 443 (admission rejects this combo)", rmsWith("http://svc/mcp", &v1alpha2.TLSConfig{}), 443},
		{"explicit port wins over tls inference", rmsWith("svc:13443/mcp", nonEmptyTLS), 13443},
		{"unparseable returns 0", rms("://"), 0},
		{"empty url returns 0", rms(""), 0},
		{"non-numeric port returns 0", rms("svc:abc/mcp"), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, EffectivePort(c.rms))
		})
	}
}

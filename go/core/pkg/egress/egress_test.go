package egress

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/assert"
)

func TestRewriteURL(t *testing.T) {
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
			assert.Equal(t, c.want, RewriteURL(c.rms))
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

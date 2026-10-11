package turn

import (
	"strings"
	"testing"
)

func TestCommandEnvironment(t *testing.T) {
	kept := []string{
		"PATH=/usr/bin", "HOME=/home/user", "USER=user", "LOGNAME=user",
		"LANG=en_US.UTF-8", "LC_ALL=C.UTF-8", "LC_CTYPE=C.UTF-8", "TERM=xterm",
		"TZ=UTC", "TMPDIR=/tmp", "SSL_CERT_FILE=/etc/ssl/ca.pem",
		"SSL_CERT_DIR=/etc/ssl/certs", "CURL_CA_BUNDLE=/etc/ssl/ca.pem",
		"REQUESTS_CA_BUNDLE=/etc/ssl/ca.pem", "GIT_SSL_CAINFO=/etc/ssl/ca.pem",
		"NODE_EXTRA_CA_CERTS=/etc/ssl/ca.pem", "AWS_CA_BUNDLE=/etc/ssl/ca.pem",
	}
	withheld := []string{
		"OPENAI_API_KEY=v", "ANTHROPIC_API_KEY=v", "AZURE_AD_TOKEN=v",
		"GOOGLE_APPLICATION_CREDENTIALS=v", "AWS_ACCESS_KEY_ID=v",
		"AWS_SECRET_ACCESS_KEY=v", "AWS_SESSION_TOKEN=v",
		"KAGENT_CONFIG_JSON={}", "KAGENT_NAME=agent", "KAGENT_API_URL=http://kagent",
		"KAGENT_POSTGRES_DATABASE_URL=postgres://u:p@db/kagent",
		"HTTPS_PROXY=http://u:p@proxy:3128", "OTEL_EXPORTER_OTLP_ENDPOINT=http://otel",
		"PWD=/runtime", "path=/lowercase", "LCX=v", "WITH_EQUALS=a=b",
	}

	environ := append(append([]string{}, withheld...), kept...)
	got := CommandEnvironment(environ, "/work")
	want := append(append([]string{}, kept...), "PWD=/work")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("CommandEnvironment() = %q, want %q", got, want)
	}

	if got := CommandEnvironment(nil, "/work"); len(got) != 1 || got[0] != "PWD=/work" {
		t.Errorf("CommandEnvironment(nil) = %q, want only PWD", got)
	}
}

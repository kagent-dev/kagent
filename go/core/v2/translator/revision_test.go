package translator

import (
	"strings"
	"testing"
)

func TestRevisionDigestIncludesSecretHashes(t *testing.T) {
	revision := &Revision{Namespace: "agents", AgentTemplateName: "helper", HarnessName: "kagent", SecretHashes: []byte("first")}
	first, err := revision.Digest()
	if err != nil {
		t.Fatal(err)
	}
	revision.SecretHashes = []byte("second")
	second, err := revision.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("secret rotation did not change runtime revision")
	}
	if len(first.Short()) != 12 || !strings.HasPrefix(first.String(), first.Short()) {
		t.Fatalf("short revision %q is not a prefix of %q", first.Short(), first.String())
	}
}

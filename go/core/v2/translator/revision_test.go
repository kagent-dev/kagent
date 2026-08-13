package translator

import "testing"

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
}

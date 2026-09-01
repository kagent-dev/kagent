package translator

import (
	"strings"
	"testing"
)

func TestRevisionDigestIncludesProvenance(t *testing.T) {
	revision := &Revision{Namespace: "agents", AgentTemplateName: "helper", HarnessName: "kagent", Provenance: []byte(`[{"kind":"Secret","hash":"first"}]`)}
	first, err := revision.Digest()
	if err != nil {
		t.Fatal(err)
	}
	revision.Provenance = []byte(`[{"kind":"Secret","hash":"second"}]`)
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

func TestCompilationWarningsDoNotAffectRevisionDigest(t *testing.T) {
	compilation := &Compilation{Revision: Revision{Namespace: "agents", AgentTemplateName: "helper", HarnessName: "claude"}}
	first, err := compilation.Digest()
	if err != nil {
		t.Fatal(err)
	}
	compilation.Warnings = []string{"partial MCP selection is not enforced"}
	if len(compilation.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one warning", compilation.Warnings)
	}
	second, err := compilation.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("non-behavioral warning changed runtime revision")
	}
}

package agentinstance

import "testing"

func TestAuthorityRoundTrip(t *testing.T) {
	const id = "8BD650A8-9775-488F-8BC1-0D52BF7BDCAB"
	authority := Authority("team-a", id)
	namespace, parsedID, err := ParseAuthority(authority + ":443")
	if err != nil {
		t.Fatal(err)
	}
	if namespace != "team-a" || parsedID != "8bd650a8-9775-488f-8bc1-0d52bf7bdcab" {
		t.Fatalf("ParseAuthority(%q) = %q/%q", authority, namespace, parsedID)
	}
}

func TestParseAuthorityRejectsNonInstanceAuthority(t *testing.T) {
	if _, _, err := ParseAuthority("actor.team-a.actors.resources.substrate.ate.dev"); err == nil {
		t.Fatal("ParseAuthority() accepted a private Actor authority")
	}
}

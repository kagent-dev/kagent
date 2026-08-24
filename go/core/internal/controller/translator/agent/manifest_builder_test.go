package agent

import "testing"

func TestBuildConfigSecretData(t *testing.T) {
	data := buildConfigSecretData(`{"app":"ok"}`, `{"card":"ok"}`)
	if data["config.json"] == "" || data["agent-card.json"] == "" {
		t.Fatal("config and agent card must be present")
	}
}

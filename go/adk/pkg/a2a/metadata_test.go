package a2a

import (
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

func TestCanonicalizeADKEvent(t *testing.T) {
	part := a2atype.NewDataPart(map[string]any{"name": "lookup"})
	part.Metadata = map[string]any{
		"adk_type":            "function_call",
		"adk_is_long_running": true,
		"adk_thought":         true,
	}
	artifact := &a2atype.Artifact{Parts: []*a2atype.Part{part}}
	event := &a2atype.TaskArtifactUpdateEvent{
		Artifact: artifact,
		Metadata: map[string]any{
			"adk_usage_metadata": map[string]any{"total_token_count": float64(3)},
			"adk_invocation_id":  "private",
		},
	}

	canonicalizeADKEvent(event)

	if part.Metadata[apia2a.PartTypeMetadataKey] != "function_call" {
		t.Fatalf("part type = %#v", part.Metadata)
	}
	if _, ok := part.Metadata["adk_is_long_running"]; ok {
		t.Fatalf("long-running adapter metadata leaked: %#v", part.Metadata)
	}
	if _, ok := event.Metadata[apia2a.UsageMetadataKey]; !ok {
		t.Fatalf("usage = %#v", event.Metadata)
	}
	for key := range part.Metadata {
		if strings.HasPrefix(key, "adk_") {
			t.Fatalf("ADK metadata leaked: %q", key)
		}
	}
	for key := range event.Metadata {
		if strings.HasPrefix(key, "adk_") {
			t.Fatalf("ADK metadata leaked: %q", key)
		}
	}
}

package a2a

import (
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"google.golang.org/adk/v2/server/adka2a/v2"
)

// canonicalizeADKEvent is the managed ADK runtime's outbound adapter boundary.
// Only public semantics cross it; other ADK implementation metadata is discarded.
func canonicalizeADKEvent(event a2atype.Event) {
	if event == nil {
		return
	}
	canonicalizeADKMetadata(event.Meta())
	switch event := event.(type) {
	case *a2atype.Message:
		canonicalizeADKMessage(event)
	case *a2atype.Task:
		canonicalizeADKMessage(event.Status.Message)
		for _, message := range event.History {
			canonicalizeADKMessage(message)
		}
		for _, artifact := range event.Artifacts {
			canonicalizeADKArtifact(artifact)
		}
	case *a2atype.TaskStatusUpdateEvent:
		canonicalizeADKMessage(event.Status.Message)
	case *a2atype.TaskArtifactUpdateEvent:
		canonicalizeADKArtifact(event.Artifact)
	}
}

func canonicalizeADKMessage(message *a2atype.Message) {
	if message == nil {
		return
	}
	canonicalizeADKMetadata(message.Metadata)
	for _, part := range message.Parts {
		canonicalizeADKPart(part)
	}
}

func canonicalizeADKArtifact(artifact *a2atype.Artifact) {
	if artifact == nil {
		return
	}
	canonicalizeADKMetadata(artifact.Metadata)
	for _, part := range artifact.Parts {
		canonicalizeADKPart(part)
	}
}

func canonicalizeADKPart(part *a2atype.Part) {
	if part == nil {
		return
	}
	metadata := part.Metadata
	copyADKMetadata(metadata, adka2a.ToA2AMetaKey(A2ADataPartMetadataTypeKey), apia2a.PartTypeMetadataKey)
	canonicalizeADKMetadata(metadata)
}

func canonicalizeADKMetadata(metadata map[string]any) {
	copyADKMetadata(metadata, adka2a.ToA2AMetaKey("usage_metadata"), apia2a.UsageMetadataKey)
	for key := range metadata {
		if strings.HasPrefix(key, "adk_") {
			delete(metadata, key)
		}
	}
}

func copyADKMetadata(metadata map[string]any, source, target string) {
	if metadata == nil {
		return
	}
	if value, ok := metadata[source]; ok {
		metadata[target] = value
	}
}

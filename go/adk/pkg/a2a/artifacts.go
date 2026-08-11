package a2a

import (
	"context"
	"fmt"
	"os"
	"strconv"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/go-logr/logr"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/server/adka2a/v2"
	"google.golang.org/adk/v2/session"
)

const (
	defaultMaxArtifactBytes = 10 * 1024 * 1024
	envMaxArtifactBytes     = "KAGENT_MAX_ARTIFACT_BYTES"
)

// MaxArtifactBytes returns the configured per-file upload and artifact limit.
func MaxArtifactBytes() int {
	if value := os.Getenv(envMaxArtifactBytes); value != "" {
		if limit, err := strconv.Atoi(value); err == nil && limit > 0 {
			return limit
		}
	}
	return defaultMaxArtifactBytes
}

func checkInboundFileSizes(message *a2atype.Message, limit int) error {
	if message == nil {
		return nil
	}
	for _, part := range message.Parts {
		if size := len(part.Raw()); size > limit {
			return fmt.Errorf("file %q exceeds maximum allowed size: %d bytes > %d bytes", part.Filename, size, limit)
		}
	}
	return nil
}

// appendSavedArtifacts surfaces files saved by tools on the A2A artifact event.
func appendSavedArtifacts(
	ctx context.Context,
	service artifact.Service,
	appName, userID, sessionID string,
	event *session.Event,
	update *a2atype.TaskArtifactUpdateEvent,
	logger logr.Logger,
) {
	if service == nil || event == nil || update == nil || len(event.Actions.ArtifactDelta) == 0 {
		return
	}

	for name, version := range event.Actions.ArtifactDelta {
		response, err := service.Load(ctx, &artifact.LoadRequest{
			AppName: appName, UserID: userID, SessionID: sessionID, FileName: name, Version: version,
		})
		if err != nil {
			logger.Error(err, "failed to load saved artifact", "name", name, "version", version)
			continue
		}
		if response == nil || response.Part == nil {
			continue
		}
		if response.Part.InlineData != nil && response.Part.InlineData.DisplayName == "" {
			response.Part.InlineData.DisplayName = name
		}
		part, err := adka2a.ToA2APart(response.Part, nil)
		if err != nil {
			logger.Error(err, "failed to convert saved artifact", "name", name, "version", version)
			continue
		}
		if part.Metadata == nil {
			part.Metadata = map[string]any{}
		}
		part.Metadata[adka2a.ToA2AMetaKey("artifact_name")] = name
		part.Metadata[adka2a.ToA2AMetaKey("artifact_version")] = version
		update.Artifact.Parts = append(update.Artifact.Parts, part)
	}
}

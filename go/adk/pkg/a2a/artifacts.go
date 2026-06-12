package a2a

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"os"
	"strconv"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	adkartifact "google.golang.org/adk/artifact"
	"google.golang.org/adk/server/adka2a" //nolint:staticcheck // kagent still uses a2a-go v1; this ADK package is the compatibility adapter.
)

const (
	// defaultMaxArtifactBytes is the default per-file size limit for inbound
	// uploads (10 MB).
	defaultMaxArtifactBytes = 10 * 1024 * 1024
	// envMaxArtifactBytes overrides the inbound file size limit (in bytes).
	envMaxArtifactBytes = "KAGENT_MAX_ARTIFACT_BYTES"
)

// MaxArtifactBytes returns the artifact size limit, honoring the
// KAGENT_MAX_ARTIFACT_BYTES env var and falling back to the default. It bounds
// both inbound uploads and agent-saved artifacts.
func MaxArtifactBytes() int {
	if v := os.Getenv(envMaxArtifactBytes); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultMaxArtifactBytes
}

// checkInboundFileSizes returns an error if any inbound FilePart's decoded
// content exceeds the limit. Only inline base64 (FileBytes) parts are checked;
// URI-referenced files are out of scope.
func checkInboundFileSizes(msg *a2atype.Message, limit int) error {
	if msg == nil {
		return nil
	}
	for _, part := range msg.Parts {
		fp := asFilePart(part)
		if fp == nil {
			continue
		}
		fb, ok := fp.File.(a2atype.FileBytes)
		if !ok {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(fb.Bytes)
		if err != nil {
			return fmt.Errorf("invalid base64 in file part %q: %w", fb.Name, err)
		}
		if len(decoded) > limit {
			return fmt.Errorf("file %q exceeds maximum allowed size: %d bytes > %d bytes", fb.Name, len(decoded), limit)
		}
	}
	return nil
}

// asFilePart extracts a *FilePart from an A2A Part, handling both value and
// pointer types.
func asFilePart(part a2atype.Part) *a2atype.FilePart {
	switch p := part.(type) {
	case *a2atype.FilePart:
		return p
	case a2atype.FilePart:
		return &p
	}
	return nil
}

// emitArtifacts loads each artifact named in delta from the artifact service
// and emits it as an A2A artifact event carrying a FilePart. Load/convert
// failures are logged and skipped so the turn continues (AC4).
func (e *KAgentExecutor) emitArtifacts(
	ctx context.Context,
	reqCtx *a2asrv.RequestContext,
	queue eventqueue.Queue,
	userID string,
	sessionID string,
	delta map[string]int64,
	eventMeta map[string]any,
) {
	svc := e.runnerConfig.ArtifactService
	if svc == nil {
		return
	}

	for name, version := range delta {
		resp, err := svc.Load(ctx, &adkartifact.LoadRequest{
			AppName:   e.appName,
			UserID:    userID,
			SessionID: sessionID,
			FileName:  name,
			Version:   version,
		})
		if err != nil {
			e.logger.Error(err, "failed to load saved artifact", "name", name, "version", version)
			continue
		}
		if resp == nil || resp.Part == nil {
			e.logger.V(1).Info("artifact load returned no part", "name", name, "version", version)
			continue
		}

		part := resp.Part
		// Carry the filename so the converted FilePart has a Name.
		if part.InlineData != nil && part.InlineData.DisplayName == "" {
			part.InlineData.DisplayName = name
		}

		a2aPart, err := adka2a.ToA2APart(part, nil)
		if err != nil {
			e.logger.Error(err, "failed to convert artifact to A2A part", "name", name, "version", version)
			continue
		}

		artifactEvent := a2atype.NewArtifactEvent(reqCtx, a2aPart)
		artifactEvent.LastChunk = true
		artifactEvent.Metadata = maps.Clone(eventMeta)
		artifactEvent.Metadata[adka2a.ToA2AMetaKey("artifact_name")] = name
		artifactEvent.Metadata[adka2a.ToA2AMetaKey("artifact_version")] = version
		if part.InlineData != nil {
			artifactEvent.Metadata[adka2a.ToA2AMetaKey("mime_type")] = part.InlineData.MIMEType
		}

		if err := queue.Write(ctx, artifactEvent); err != nil {
			e.logger.Error(err, "failed to write artifact event", "name", name, "version", version)
		}
	}
}

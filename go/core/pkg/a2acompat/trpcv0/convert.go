package trpcv0

import (
	"encoding/json"
	"fmt"
	"maps"
	"time"

	legacya2a "github.com/a2aproject/a2a-go/a2a"
	a2av1 "github.com/a2aproject/a2a-go/v2/a2a"
	a2av0 "github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
	trpc "trpc.group/trpc-go/trpc-a2a-go/protocol"
)

const ProtocolVersionV1 = string(a2av1.Version)

// TaskJSONToV1JSON converts a persisted trpc-a2a-go task blob to official A2A v1 JSON.
func TaskJSONToV1JSON(data []byte) ([]byte, error) {
	var task trpc.Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("unmarshal trpc task: %w", err)
	}
	v1, err := ToV1Task(&task)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v1)
}

// PushNotificationJSONToV1JSON converts a persisted trpc-a2a-go push config blob to official A2A v1 JSON.
func PushNotificationJSONToV1JSON(data []byte) ([]byte, error) {
	var cfg trpc.TaskPushNotificationConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal trpc push notification config: %w", err)
	}
	v1 := ToV1PushConfig(&cfg)
	return json.Marshal(v1)
}

func ToV1Task(task *trpc.Task) (*a2av1.Task, error) {
	v0, err := ToOfficialV0Task(task)
	if err != nil {
		return nil, err
	}
	return a2av0.ToV1Task(v0)
}

func ToV1PushConfig(cfg *trpc.TaskPushNotificationConfig) *a2av1.PushConfig {
	return a2av0.ToV1PushConfig(ToOfficialV0PushConfig(cfg))
}

func ToLegacyTask(task *a2av1.Task) (*trpc.Task, error) {
	if task == nil {
		return nil, nil
	}
	message, err := toLegacyMessage(task.Status.Message)
	if err != nil {
		return nil, fmt.Errorf("convert status message: %w", err)
	}

	result := &trpc.Task{
		ID:        string(task.ID),
		ContextID: task.ContextID,
		Metadata:  task.Metadata,
		Status: trpc.TaskStatus{
			State:     toLegacyTaskState(task.Status.State),
			Message:   message,
			Timestamp: formatTimestamp(task.Status.Timestamp),
		},
	}

	if len(task.History) > 0 {
		result.History = make([]trpc.Message, 0, len(task.History))
		for i := range task.History {
			msg, convErr := toLegacyMessage(task.History[i])
			if convErr != nil {
				return nil, fmt.Errorf("convert history message %d: %w", i, convErr)
			}
			if msg != nil {
				result.History = append(result.History, *msg)
			}
		}
	}
	if len(task.Artifacts) > 0 {
		result.Artifacts = make([]trpc.Artifact, 0, len(task.Artifacts))
		for i := range task.Artifacts {
			artifact, convErr := toLegacyArtifact(task.Artifacts[i])
			if convErr != nil {
				return nil, fmt.Errorf("convert artifact %d: %w", i, convErr)
			}
			if artifact != nil {
				result.Artifacts = append(result.Artifacts, *artifact)
			}
		}
	}
	return result, nil
}

func ToLegacyPushConfig(cfg *a2av1.PushConfig) *trpc.TaskPushNotificationConfig {
	if cfg == nil {
		return nil
	}
	result := &trpc.TaskPushNotificationConfig{
		TaskID: string(cfg.TaskID),
		PushNotificationConfig: trpc.PushNotificationConfig{
			ID:    cfg.ID,
			URL:   cfg.URL,
			Token: cfg.Token,
		},
	}
	if cfg.Auth != nil {
		credentials := cfg.Auth.Credentials
		schemes := []string{}
		if cfg.Auth.Scheme != "" {
			schemes = append(schemes, cfg.Auth.Scheme)
		}
		result.PushNotificationConfig.Authentication = &trpc.AuthenticationInfo{
			Credentials: &credentials,
			Schemes:     schemes,
		}
	}
	return result
}

func ToOfficialV0Task(task *trpc.Task) (*legacya2a.Task, error) {
	if task == nil {
		return nil, nil
	}
	status, err := ToOfficialV0TaskStatus(task.Status)
	if err != nil {
		return nil, err
	}
	result := &legacya2a.Task{
		ID:        legacya2a.TaskID(task.ID),
		ContextID: task.ContextID,
		Metadata:  task.Metadata,
		Status:    status,
	}
	if len(task.History) > 0 {
		result.History = make([]*legacya2a.Message, len(task.History))
		for i := range task.History {
			result.History[i], err = ToOfficialV0Message(&task.History[i])
			if err != nil {
				return nil, fmt.Errorf("convert task history message %d: %w", i, err)
			}
		}
	}
	if len(task.Artifacts) > 0 {
		result.Artifacts = make([]*legacya2a.Artifact, len(task.Artifacts))
		for i := range task.Artifacts {
			result.Artifacts[i], err = ToOfficialV0Artifact(&task.Artifacts[i])
			if err != nil {
				return nil, fmt.Errorf("convert task artifact %d: %w", i, err)
			}
		}
	}
	return result, nil
}

func toLegacyMessage(message *a2av1.Message) (*trpc.Message, error) {
	if message == nil {
		return nil, nil
	}
	parts, err := toLegacyParts(message.Parts)
	if err != nil {
		return nil, err
	}

	taskID := ""
	if message.TaskID != "" {
		taskID = string(message.TaskID)
	}
	contextID := message.ContextID

	result := &trpc.Message{
		Kind:             trpc.KindMessage,
		MessageID:        message.ID,
		ContextID:        new(contextID),
		Extensions:       message.Extensions,
		Metadata:         message.Metadata,
		Parts:            parts,
		ReferenceTaskIDs: toLegacyTaskIDs(message.ReferenceTasks),
		Role:             toLegacyMessageRole(message.Role),
		TaskID:           new(taskID),
	}
	return result, nil
}

func toLegacyArtifact(artifact *a2av1.Artifact) (*trpc.Artifact, error) {
	if artifact == nil {
		return nil, nil
	}
	parts, err := toLegacyParts(artifact.Parts)
	if err != nil {
		return nil, err
	}
	id := string(artifact.ID)
	name := artifact.Name
	description := artifact.Description

	return &trpc.Artifact{
		ArtifactID:  id,
		Name:        new(name),
		Description: new(description),
		Metadata:    artifact.Metadata,
		Extensions:  artifact.Extensions,
		Parts:       parts,
	}, nil
}

func toLegacyParts(parts a2av1.ContentParts) ([]trpc.Part, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	result := make([]trpc.Part, 0, len(parts))
	for i := range parts {
		part, err := toLegacyPart(parts[i])
		if err != nil {
			return nil, fmt.Errorf("convert part %d: %w", i, err)
		}
		result = append(result, part)
	}
	return result, nil
}

func toLegacyPart(part *a2av1.Part) (trpc.Part, error) {
	if part == nil {
		return trpc.TextPart{Kind: trpc.KindText, Text: ""}, nil
	}
	if text := part.Text(); text != "" {
		return trpc.TextPart{
			Kind:     trpc.KindText,
			Text:     text,
			Metadata: part.Metadata,
		}, nil
	}
	if data := part.Data(); data != nil {
		return trpc.DataPart{
			Kind:     trpc.KindData,
			Data:     data,
			Metadata: part.Metadata,
		}, nil
	}
	if url := part.URL(); url != "" {
		urlString := string(url)
		fileName := part.Filename
		mimeType := part.MediaType
		return trpc.FilePart{
			Kind: trpc.KindFile,
			File: &trpc.FileWithURI{
				Name:     new(fileName),
				MimeType: new(mimeType),
				URI:      urlString,
			},
			Metadata: part.Metadata,
		}, nil
	}
	raw := part.Raw()
	if len(raw) > 0 {
		fileName := part.Filename
		mimeType := part.MediaType
		return trpc.FilePart{
			Kind: trpc.KindFile,
			File: &trpc.FileWithBytes{
				Name:     new(fileName),
				MimeType: new(mimeType),
				Bytes:    string(raw),
			},
			Metadata: part.Metadata,
		}, nil
	}
	return trpc.DataPart{
		Kind:     trpc.KindData,
		Data:     map[string]any{},
		Metadata: part.Metadata,
	}, nil
}

func toLegacyTaskState(state a2av1.TaskState) trpc.TaskState {
	switch state {
	case a2av1.TaskStateSubmitted:
		return trpc.TaskStateSubmitted
	case a2av1.TaskStateWorking:
		return trpc.TaskStateWorking
	case a2av1.TaskStateInputRequired:
		return trpc.TaskStateInputRequired
	case a2av1.TaskStateCompleted:
		return trpc.TaskStateCompleted
	case a2av1.TaskStateCanceled:
		return trpc.TaskStateCanceled
	case a2av1.TaskStateFailed:
		return trpc.TaskStateFailed
	case a2av1.TaskStateRejected:
		return trpc.TaskStateRejected
	case a2av1.TaskStateAuthRequired:
		return trpc.TaskStateAuthRequired
	default:
		return trpc.TaskStateUnknown
	}
}

func toLegacyMessageRole(role a2av1.MessageRole) trpc.MessageRole {
	switch role {
	case a2av1.MessageRoleAgent:
		return trpc.MessageRoleAgent
	case a2av1.MessageRoleUser:
		return trpc.MessageRoleUser
	default:
		return trpc.MessageRoleAgent
	}
}

func toLegacyTaskIDs(ids []a2av1.TaskID) []string {
	if len(ids) == 0 {
		return nil
	}
	result := make([]string, len(ids))
	for i := range ids {
		result[i] = string(ids[i])
	}
	return result
}

func formatTimestamp(timestamp *time.Time) string {
	if timestamp == nil {
		return ""
	}
	return timestamp.UTC().Format(time.RFC3339Nano)
}

func ToOfficialV0TaskStatus(status trpc.TaskStatus) (legacya2a.TaskStatus, error) {
	var msg *legacya2a.Message
	var err error
	if status.Message != nil {
		msg, err = ToOfficialV0Message(status.Message)
		if err != nil {
			return legacya2a.TaskStatus{}, fmt.Errorf("convert task status message: %w", err)
		}
	}
	timestamp, err := parseTimestamp(status.Timestamp)
	if err != nil {
		return legacya2a.TaskStatus{}, err
	}
	return legacya2a.TaskStatus{
		Message:   msg,
		State:     ToOfficialV0TaskState(status.State),
		Timestamp: timestamp,
	}, nil
}

func ToOfficialV0Message(message *trpc.Message) (*legacya2a.Message, error) {
	if message == nil {
		return nil, nil
	}
	parts, err := ToOfficialV0Parts(message.Parts)
	if err != nil {
		return nil, err
	}
	return &legacya2a.Message{
		ID:             message.MessageID,
		ContextID:      derefString(message.ContextID),
		Extensions:     message.Extensions,
		Metadata:       message.Metadata,
		Parts:          parts,
		ReferenceTasks: toOfficialV0TaskIDs(message.ReferenceTaskIDs),
		Role:           ToOfficialV0MessageRole(message.Role),
		TaskID:         legacya2a.TaskID(derefString(message.TaskID)),
	}, nil
}

func ToOfficialV0Artifact(artifact *trpc.Artifact) (*legacya2a.Artifact, error) {
	if artifact == nil {
		return nil, nil
	}
	parts, err := ToOfficialV0Parts(artifact.Parts)
	if err != nil {
		return nil, err
	}
	return &legacya2a.Artifact{
		ID:          legacya2a.ArtifactID(artifact.ArtifactID),
		Description: derefString(artifact.Description),
		Extensions:  artifact.Extensions,
		Metadata:    artifact.Metadata,
		Name:        derefString(artifact.Name),
		Parts:       parts,
	}, nil
}

func ToOfficialV0Parts(parts []trpc.Part) (legacya2a.ContentParts, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	result := make(legacya2a.ContentParts, len(parts))
	for i, part := range parts {
		converted, err := ToOfficialV0Part(part)
		if err != nil {
			return nil, fmt.Errorf("convert part %d: %w", i, err)
		}
		result[i] = converted
	}
	return result, nil
}

func ToOfficialV0Part(part trpc.Part) (legacya2a.Part, error) {
	switch p := part.(type) {
	case nil:
		return nil, nil
	case trpc.TextPart:
		return legacya2a.TextPart{Text: p.Text, Metadata: p.Metadata}, nil
	case *trpc.TextPart:
		return legacya2a.TextPart{Text: p.Text, Metadata: p.Metadata}, nil
	case trpc.DataPart:
		return toOfficialV0DataPart(p), nil
	case *trpc.DataPart:
		return toOfficialV0DataPart(*p), nil
	case trpc.FilePart:
		return ToOfficialV0FilePart(p)
	case *trpc.FilePart:
		return ToOfficialV0FilePart(*p)
	default:
		return nil, fmt.Errorf("unsupported trpc part type %T", part)
	}
}

func ToOfficialV0FilePart(part trpc.FilePart) (legacya2a.Part, error) {
	switch file := part.File.(type) {
	case nil:
		return nil, fmt.Errorf("file part missing file payload")
	case *trpc.FileWithBytes:
		return officialV0FileBytes(*file, part.Metadata), nil
	case *trpc.FileWithURI:
		return officialV0FileURI(*file, part.Metadata), nil
	default:
		return nil, fmt.Errorf("unsupported trpc file payload type %T", part.File)
	}
}

func ToOfficialV0PushConfig(cfg *trpc.TaskPushNotificationConfig) *legacya2a.TaskPushConfig {
	if cfg == nil {
		return nil
	}
	pushConfig := legacya2a.PushConfig{
		ID:    cfg.PushNotificationConfig.ID,
		Token: cfg.PushNotificationConfig.Token,
		URL:   cfg.PushNotificationConfig.URL,
	}
	if cfg.PushNotificationConfig.Authentication != nil {
		pushConfig.Auth = &legacya2a.PushAuthInfo{
			Credentials: derefString(cfg.PushNotificationConfig.Authentication.Credentials),
			Schemes:     cfg.PushNotificationConfig.Authentication.Schemes,
		}
	}
	return &legacya2a.TaskPushConfig{
		Config: pushConfig,
		TaskID: legacya2a.TaskID(cfg.TaskID),
	}
}

func ToOfficialV0TaskState(state trpc.TaskState) legacya2a.TaskState {
	switch state {
	case trpc.TaskStateSubmitted:
		return legacya2a.TaskStateSubmitted
	case trpc.TaskStateWorking:
		return legacya2a.TaskStateWorking
	case trpc.TaskStateInputRequired:
		return legacya2a.TaskStateInputRequired
	case trpc.TaskStateCompleted:
		return legacya2a.TaskStateCompleted
	case trpc.TaskStateCanceled:
		return legacya2a.TaskStateCanceled
	case trpc.TaskStateFailed:
		return legacya2a.TaskStateFailed
	case trpc.TaskStateRejected:
		return legacya2a.TaskStateRejected
	case trpc.TaskStateAuthRequired:
		return legacya2a.TaskStateAuthRequired
	case trpc.TaskStateUnknown:
		return legacya2a.TaskStateUnknown
	default:
		return legacya2a.TaskStateUnspecified
	}
}

func ToOfficialV0MessageRole(role trpc.MessageRole) legacya2a.MessageRole {
	switch role {
	case trpc.MessageRoleAgent:
		return legacya2a.MessageRoleAgent
	case trpc.MessageRoleUser:
		return legacya2a.MessageRoleUser
	default:
		return legacya2a.MessageRoleUnspecified
	}
}

func toOfficialV0DataPart(part trpc.DataPart) legacya2a.DataPart {
	data, ok := part.Data.(map[string]any)
	metadata := maps.Clone(part.Metadata)
	if !ok {
		data = map[string]any{"value": part.Data}
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["data_part_compat"] = true
	}
	return legacya2a.DataPart{Data: data, Metadata: metadata}
}

func officialV0FileBytes(file trpc.FileWithBytes, metadata map[string]any) legacya2a.FilePart {
	return legacya2a.FilePart{
		File: legacya2a.FileBytes{
			FileMeta: legacya2a.FileMeta{
				MimeType: derefString(file.MimeType),
				Name:     derefString(file.Name),
			},
			Bytes: file.Bytes,
		},
		Metadata: metadata,
	}
}

func officialV0FileURI(file trpc.FileWithURI, metadata map[string]any) legacya2a.FilePart {
	return legacya2a.FilePart{
		File: legacya2a.FileURI{
			FileMeta: legacya2a.FileMeta{
				MimeType: derefString(file.MimeType),
				Name:     derefString(file.Name),
			},
			URI: file.URI,
		},
		Metadata: metadata,
	}
}

func parseTimestamp(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, fmt.Errorf("parse task status timestamp %q: %w", raw, err)
	}
	return &parsed, nil
}

func toOfficialV0TaskIDs(ids []string) []legacya2a.TaskID {
	if len(ids) == 0 {
		return nil
	}
	result := make([]legacya2a.TaskID, len(ids))
	for i, id := range ids {
		result[i] = legacya2a.TaskID(id)
	}
	return result
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

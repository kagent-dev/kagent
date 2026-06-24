package trpcv0

import (
	"encoding/json"
	"testing"
	"time"

	a2av1 "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
	trpc "trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func TestTaskJSONToV1JSON_ClusterTextTask(t *testing.T) {
	task := mustConvertTaskJSONToV1(t, buildLegacyTextTaskFixture())
	assertForwardTextTaskFixture(t, task)
}

func TestTaskJSONToV1JSON_ClusterDataTask(t *testing.T) {
	task := mustConvertTaskJSONToV1(t, buildLegacyDataTaskFixture())
	assertForwardDataTaskFixture(t, task)
}

func TestPushNotificationJSONToV1JSON(t *testing.T) {
	cfg := mustConvertPushNotificationJSONToV1(t, buildLegacyPushConfigFixture())

	want := a2av1.PushConfig{
		TaskID: "task-1",
		ID:     "cfg-1",
		URL:    "https://callback.example",
		Token:  "tok",
		Auth: &a2av1.PushAuthInfo{
			Credentials: "cred",
			Scheme:      "Bearer",
		},
	}
	if diff := cmp.Diff(want, cfg); diff != "" {
		t.Fatalf("unexpected push config (-want +got):\n%s", diff)
	}
}

func TestToLegacyTask_FromV1RichFixture(t *testing.T) {
	v1Task := buildV1RichTaskFixture()
	got := mustConvertToLegacyTask(t, v1Task)
	assertBackwardTaskFixture(t, got)
}

func TestToLegacyPushConfig_FromV1(t *testing.T) {
	got := ToLegacyPushConfig(buildV1PushConfigFixture())
	if got == nil {
		t.Fatal("expected non-nil config")
	}
	if got.TaskID != "task-v1-rich" || got.PushNotificationConfig.ID != "cfg-v1" || got.PushNotificationConfig.URL != "https://callback.example/v1" || got.PushNotificationConfig.Token != "token-v1" {
		t.Fatalf("unexpected legacy push config: %+v", got)
	}
	if got.PushNotificationConfig.Authentication == nil {
		t.Fatal("expected authentication")
	}
	if got.PushNotificationConfig.Authentication.Credentials == nil || *got.PushNotificationConfig.Authentication.Credentials != "secret" {
		t.Fatalf("credentials = %v", got.PushNotificationConfig.Authentication.Credentials)
	}
	if len(got.PushNotificationConfig.Authentication.Schemes) != 1 || got.PushNotificationConfig.Authentication.Schemes[0] != "Bearer" {
		t.Fatalf("schemes = %+v", got.PushNotificationConfig.Authentication.Schemes)
	}
}

func mustConvertTaskJSONToV1(t *testing.T, fixture trpc.Task) a2av1.Task {
	t.Helper()
	input, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal legacy task fixture: %v", err)
	}
	data, err := TaskJSONToV1JSON(input)
	if err != nil {
		t.Fatalf("TaskJSONToV1JSON() error = %v", err)
	}
	var task a2av1.Task
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatalf("unmarshal v1 task: %v\njson: %s", err, data)
	}
	return task
}

func mustConvertPushNotificationJSONToV1(t *testing.T, fixture trpc.TaskPushNotificationConfig) a2av1.PushConfig {
	t.Helper()
	input, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal legacy push fixture: %v", err)
	}
	data, err := PushNotificationJSONToV1JSON(input)
	if err != nil {
		t.Fatalf("PushNotificationJSONToV1JSON() error = %v", err)
	}
	var cfg a2av1.PushConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal v1 push config: %v", err)
	}
	return cfg
}

func mustConvertToLegacyTask(t *testing.T, fixture *a2av1.Task) *trpc.Task {
	t.Helper()
	got, err := ToLegacyTask(fixture)
	if err != nil {
		t.Fatalf("ToLegacyTask() error = %v", err)
	}
	return got
}

func assertForwardTextTaskFixture(t *testing.T, task a2av1.Task) {
	t.Helper()
	if task.ID != "019d49ab-6830-763c-9db6-1b6359228c4c" {
		t.Fatalf("task ID = %q", task.ID)
	}
	if task.Status.State != a2av1.TaskStateCompleted {
		t.Fatalf("task state = %q", task.Status.State)
	}
	if got := task.History[0].Role; got != a2av1.MessageRoleUser {
		t.Fatalf("history role = %q", got)
	}
	if got := task.History[0].Parts[0].Text(); got != "hi" {
		t.Fatalf("history text part = %q", got)
	}
	if got := task.Artifacts[0].Parts[0].Text(); got != "Hello! How can I assist you with Kubernetes today?" {
		t.Fatalf("artifact text part = %q", got)
	}
}

func assertForwardDataTaskFixture(t *testing.T, task a2av1.Task) {
	t.Helper()
	if task.Status.State != a2av1.TaskStateInputRequired {
		t.Fatalf("task state = %q", task.Status.State)
	}
	if task.Status.Message == nil {
		t.Fatal("expected status message")
	}
	dataPart, ok := task.Status.Message.Parts[0].Data().(map[string]any)
	if !ok {
		t.Fatalf("status message part data type = %T", task.Status.Message.Parts[0].Data())
	}
	if got := dataPart["name"]; got != "adk_request_confirmation" {
		t.Fatalf("status message data name = %v", got)
	}
}

func assertBackwardTaskFixture(t *testing.T, got *trpc.Task) {
	t.Helper()
	if got.Kind != trpc.KindTask {
		t.Fatalf("task kind = %q, want %q", got.Kind, trpc.KindTask)
	}
	if got.ID != "task-v1-rich" {
		t.Fatalf("task ID = %q", got.ID)
	}
	if got.ContextID != "ctx-bridge" {
		t.Fatalf("context ID = %q", got.ContextID)
	}
	if got.Status.State != trpc.TaskStateWorking {
		t.Fatalf("status state = %q", got.Status.State)
	}
	wantTS := time.Date(2026, time.January, 2, 3, 4, 5, 123456000, time.UTC).Format(time.RFC3339Nano)
	if got.Status.Timestamp != wantTS {
		t.Fatalf("status timestamp = %q", got.Status.Timestamp)
	}
	if got.Status.Message == nil {
		t.Fatal("expected status message")
	}
	if got.Status.Message.MessageID == "" {
		t.Fatal("expected non-empty status message ID")
	}
	if got.Status.Message.TaskID == nil || *got.Status.Message.TaskID != "task-v1-rich" {
		t.Fatalf("status message task ID = %v", got.Status.Message.TaskID)
	}

	if len(got.History) != 1 {
		t.Fatalf("history length = %d", len(got.History))
	}
	if len(got.History[0].Parts) != 4 {
		t.Fatalf("history parts length = %d", len(got.History[0].Parts))
	}

	textPart := mustTextPart(t, got.History[0].Parts[0])
	if textPart.Kind != trpc.KindText || textPart.Text != "hello" {
		t.Fatalf("unexpected text part: %+v", textPart)
	}
	dataPart := mustDataPart(t, got.History[0].Parts[1])
	dataMap, ok := dataPart.Data.(map[string]any)
	if !ok {
		t.Fatalf("data part payload type = %T", dataPart.Data)
	}
	if dataMap["step"] != float64(1) {
		t.Fatalf("data part step = %v", dataMap["step"])
	}
	urlFilePart := mustFilePart(t, got.History[0].Parts[2])
	urlFile, ok := urlFilePart.File.(*trpc.FileWithURI)
	if !ok {
		t.Fatalf("expected FileWithURI, got %T", urlFilePart.File)
	}
	if urlFile.URI != "https://example.com/doc.md" {
		t.Fatalf("file URI = %q", urlFile.URI)
	}
	rawFilePart := mustFilePart(t, got.History[0].Parts[3])
	rawFile, ok := rawFilePart.File.(*trpc.FileWithBytes)
	if !ok {
		t.Fatalf("expected FileWithBytes, got %T", rawFilePart.File)
	}
	if rawFile.Bytes != "RAW_BYTES" {
		t.Fatalf("raw bytes = %q", rawFile.Bytes)
	}

	if len(got.Artifacts) != 1 || len(got.Artifacts[0].Parts) != 1 {
		t.Fatalf("unexpected artifacts: %+v", got.Artifacts)
	}
	if gotArtifactText := mustTextPart(t, got.Artifacts[0].Parts[0]).Text; gotArtifactText != "artifact-text" {
		t.Fatalf("artifact text = %q", gotArtifactText)
	}
}

func mustTextPart(t *testing.T, part trpc.Part) trpc.TextPart {
	t.Helper()
	switch p := part.(type) {
	case trpc.TextPart:
		return p
	case *trpc.TextPart:
		return *p
	default:
		t.Fatalf("expected TextPart, got %T", part)
		return trpc.TextPart{}
	}
}

func mustDataPart(t *testing.T, part trpc.Part) trpc.DataPart {
	t.Helper()
	switch p := part.(type) {
	case trpc.DataPart:
		return p
	case *trpc.DataPart:
		return *p
	default:
		t.Fatalf("expected DataPart, got %T", part)
		return trpc.DataPart{}
	}
}

func mustFilePart(t *testing.T, part trpc.Part) trpc.FilePart {
	t.Helper()
	switch p := part.(type) {
	case trpc.FilePart:
		return p
	case *trpc.FilePart:
		return *p
	default:
		t.Fatalf("expected FilePart, got %T", part)
		return trpc.FilePart{}
	}
}

func buildLegacyTextTaskFixture() trpc.Task {
	return trpc.Task{
		ID:        "019d49ab-6830-763c-9db6-1b6359228c4c",
		Kind:      trpc.KindTask,
		ContextID: "ctx-text",
		Status: trpc.TaskStatus{
			State: trpc.TaskStateCompleted,
			Message: &trpc.Message{
				Kind:      trpc.KindMessage,
				MessageID: "msg-status-1",
				Role:      trpc.MessageRoleAgent,
				Parts: []trpc.Part{
					trpc.TextPart{Kind: trpc.KindText, Text: "done"},
				},
			},
		},
		History: []trpc.Message{
			{
				Kind:      trpc.KindMessage,
				MessageID: "msg-user-1",
				Role:      trpc.MessageRoleUser,
				Parts: []trpc.Part{
					trpc.TextPart{Kind: trpc.KindText, Text: "hi"},
				},
			},
		},
		Artifacts: []trpc.Artifact{
			{
				ArtifactID: "artifact-1",
				Parts: []trpc.Part{
					trpc.TextPart{Kind: trpc.KindText, Text: "Hello! How can I assist you with Kubernetes today?"},
				},
			},
		},
	}
}

func buildLegacyDataTaskFixture() trpc.Task {
	return trpc.Task{
		ID:        "task-data-1",
		Kind:      trpc.KindTask,
		ContextID: "ctx-data",
		Status: trpc.TaskStatus{
			State: trpc.TaskStateInputRequired,
			Message: &trpc.Message{
				Kind:      trpc.KindMessage,
				MessageID: "msg-status-data-1",
				Role:      trpc.MessageRoleAgent,
				Parts: []trpc.Part{
					trpc.DataPart{
						Kind: trpc.KindData,
						Data: map[string]any{
							"name": "adk_request_confirmation",
						},
					},
				},
			},
		},
	}
}

func buildLegacyPushConfigFixture() trpc.TaskPushNotificationConfig {
	cred := "cred"
	return trpc.TaskPushNotificationConfig{
		TaskID: "task-1",
		PushNotificationConfig: trpc.PushNotificationConfig{
			ID:    "cfg-1",
			URL:   "https://callback.example",
			Token: "tok",
			Authentication: &trpc.AuthenticationInfo{
				Credentials: &cred,
				Schemes:     []string{"Bearer"},
			},
		},
	}
}

func buildV1RichTaskFixture() *a2av1.Task {
	ts := time.Date(2026, time.January, 2, 3, 4, 5, 123456000, time.UTC)
	taskID := a2av1.TaskID("task-v1-rich")
	fileURLPart := a2av1.NewFileURLPart(a2av1.URL("https://example.com/doc.md"), "text/markdown")
	fileURLPart.Filename = "doc.md"
	fileURLPart.Metadata = map[string]any{"source": "url"}
	rawPart := a2av1.NewRawPart([]byte("RAW_BYTES"))
	rawPart.Filename = "blob.bin"
	rawPart.MediaType = "application/octet-stream"
	rawPart.Metadata = map[string]any{"source": "raw"}

	statusMessage := a2av1.NewMessage(a2av1.MessageRoleAgent, a2av1.NewTextPart("working"))
	statusMessage.TaskID = taskID
	statusMessage.ContextID = "ctx-bridge"

	historyMessage := a2av1.NewMessage(
		a2av1.MessageRoleUser,
		a2av1.NewTextPart("hello"),
		a2av1.NewDataPart(map[string]any{"step": float64(1)}),
		fileURLPart,
		rawPart,
	)
	historyMessage.TaskID = taskID
	historyMessage.ContextID = "ctx-bridge"

	return &a2av1.Task{
		ID:        taskID,
		ContextID: "ctx-bridge",
		Metadata:  map[string]any{"kagent": "true"},
		Status: a2av1.TaskStatus{
			State:     a2av1.TaskStateWorking,
			Timestamp: &ts,
			Message:   statusMessage,
		},
		History: []*a2av1.Message{historyMessage},
		Artifacts: []*a2av1.Artifact{
			{
				ID:    "artifact-v1",
				Parts: a2av1.ContentParts{a2av1.NewTextPart("artifact-text")},
			},
		},
	}
}

func buildV1PushConfigFixture() *a2av1.PushConfig {
	return &a2av1.PushConfig{
		TaskID: "task-v1-rich",
		ID:     "cfg-v1",
		URL:    "https://callback.example/v1",
		Token:  "token-v1",
		Auth: &a2av1.PushAuthInfo{
			Credentials: "secret",
			Scheme:      "Bearer",
		},
	}
}

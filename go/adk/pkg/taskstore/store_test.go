package taskstore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCreatePreservesTaskHistoryAndArtifacts(t *testing.T) {
	partialMessage := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("partial text"))
	partialMessage.Metadata = map[string]any{"adk_partial": true}
	emptyMessage := a2atype.NewMessage(a2atype.MessageRoleAgent)

	task := &a2atype.Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		Status:    a2atype.TaskStatus{State: a2atype.TaskStateWorking},
		History:   []*a2atype.Message{partialMessage, emptyMessage},
		Artifacts: []*a2atype.Artifact{
			{
				ID:       "artifact-1",
				Parts:    a2atype.ContentParts{a2atype.NewTextPart("partial artifact")},
				Metadata: map[string]any{"kagent_adk_partial": true},
			},
			{ID: "artifact-close", Parts: a2atype.ContentParts{}},
		},
	}

	var saved a2atype.Task
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(req.Body).Decode(&saved); err != nil {
			t.Fatalf("decode saved task: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})}

	store := NewKAgentTaskStoreWithClient("http://kagent.test", client)
	if _, err := store.Create(context.Background(), task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if len(saved.History) != 2 {
		t.Fatalf("saved history len = %d, want 2", len(saved.History))
	}
	if partial, _ := saved.History[0].Metadata["adk_partial"].(bool); !partial {
		t.Fatalf("saved partial message metadata = %#v, want adk_partial=true", saved.History[0].Metadata)
	}
	if len(saved.History[1].Parts) != 0 {
		t.Fatalf("saved empty message parts len = %d, want 0", len(saved.History[1].Parts))
	}

	if len(saved.Artifacts) != 2 {
		t.Fatalf("saved artifacts len = %d, want 2", len(saved.Artifacts))
	}
	if partial, _ := saved.Artifacts[0].Metadata["kagent_adk_partial"].(bool); !partial {
		t.Fatalf("saved partial artifact metadata = %#v, want kagent_adk_partial=true", saved.Artifacts[0].Metadata)
	}
	if len(saved.Artifacts[1].Parts) != 0 {
		t.Fatalf("saved empty artifact parts len = %d, want 0", len(saved.Artifacts[1].Parts))
	}
}

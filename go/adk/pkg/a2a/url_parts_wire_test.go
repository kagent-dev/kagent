package a2a

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/fileextract"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// A non-image link fails its own turn, but the session keeps working afterwards.
func TestChatCompletionsLinkFailsOnlyItsOwnTurn(t *testing.T) {
	ctx := t.Context()
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoded, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(encoded, &body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-4o",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]
		}`)
	}))
	defer server.Close()

	client := openai.NewClient(
		option.WithAPIKey("test"),
		option.WithBaseURL(server.URL),
		option.WithHTTPClient(server.Client()),
	)
	llm := &models.OpenAIModel{
		Config: &models.OpenAIConfig{Model: "gpt-4o"},
		Client: client,
		Logger: slog.New(slog.DiscardHandler),
	}
	a, err := llmagent.New(llmagent.Config{Name: "links", Model: fileextract.WithFileText(llm)})
	if err != nil {
		t.Fatal(err)
	}
	sessions := adksession.InMemoryService()
	r, err := runner.New(runner.Config{AppName: "test", Agent: a, SessionService: sessions})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, &adksession.CreateRequest{AppName: "test", UserID: "user"})
	if err != nil {
		t.Fatal(err)
	}
	send := func(parts ...*a2atype.Part) error {
		t.Helper()
		msg := &genai.Content{Role: genai.RoleUser}
		for _, p := range parts {
			converted, err := a2aPartConverter(ctx, nil, p)
			if err != nil {
				t.Fatal(err)
			}
			msg.Parts = append(msg.Parts, converted)
		}
		for _, err := range r.Run(ctx, "user", sess.Session.ID(), msg, adkagent.RunConfig{}) {
			if err != nil {
				return err
			}
		}
		return nil
	}

	const link = "https://example.com/report.pdf"
	err = send(a2atype.NewTextPart("read this"), a2atype.NewFileURLPart(link, "application/pdf"))
	if err == nil || !strings.Contains(err.Error(), link) {
		t.Fatalf("first turn error = %v, want one that names %q", err, link)
	}
	if len(bodies) != 0 {
		t.Fatalf("model called %d times on the first turn, want 0", len(bodies))
	}

	if err := send(a2atype.NewTextPart("hello again")); err != nil {
		t.Fatalf("second turn error = %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("model called %d times, want 1", len(bodies))
	}
	var users []string
	messages, _ := bodies[0]["messages"].([]any)
	for _, m := range messages {
		if msg, _ := m.(map[string]any); msg["role"] == "user" {
			content, _ := msg["content"].(string)
			users = append(users, content)
		}
	}
	if len(users) < 2 {
		t.Fatalf("messages = %#v, want at least two user messages", messages)
	}
	if want := `[Link "` + link + `" was not sent`; !strings.Contains(users[0], want) {
		t.Errorf("first user message = %q, want it to contain %q", users[0], want)
	}
	if got := users[len(users)-1]; got != "hello again" {
		t.Errorf("last user message = %q, want %q", got, "hello again")
	}
}

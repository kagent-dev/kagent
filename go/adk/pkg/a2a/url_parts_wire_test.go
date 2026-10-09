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

// chatCompletionsHarness wires the real Chat Completions adapter the way production
// does (file wrapper, agent, runner, one session) in front of a fake provider. It
// returns a sender for A2A parts and the request bodies the provider received.
func chatCompletionsHarness(t *testing.T) (send func(parts ...*a2atype.Part) error, bodies *[]map[string]any) {
	t.Helper()
	ctx := t.Context()
	var got []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoded, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(encoded, &body)
		got = append(got, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-4o",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]
		}`)
	}))
	t.Cleanup(server.Close)

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
	a, err := llmagent.New(llmagent.Config{Name: "files", Model: fileextract.WithFileText(llm)})
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
	send = func(parts ...*a2atype.Part) error {
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
	return send, &got
}

// A non-image link fails its own turn, but the session keeps working afterwards.
func TestChatCompletionsLinkFailsOnlyItsOwnTurn(t *testing.T) {
	send, bodies := chatCompletionsHarness(t)

	const link = "https://example.com/report.pdf"
	err := send(a2atype.NewTextPart("read this"), a2atype.NewFileURLPart(link, "application/pdf"))
	if err == nil || !strings.Contains(err.Error(), link) {
		t.Fatalf("first turn error = %v, want one that names %q", err, link)
	}
	if len(*bodies) != 0 {
		t.Fatalf("model called %d times on the first turn, want 0", len(*bodies))
	}

	if err := send(a2atype.NewTextPart("hello again")); err != nil {
		t.Fatalf("second turn error = %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("model called %d times, want 1", len(*bodies))
	}
	var users []string
	messages, _ := (*bodies)[0]["messages"].([]any)
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

// A PDF passes the file wrapper unchanged and goes out as a Chat Completions file part.
func TestChatCompletionsSendsPDFBytesAsFilePart(t *testing.T) {
	send, bodies := chatCompletionsHarness(t)

	if err := send(a2atype.NewTextPart("summarize"), fileA2APart("report.pdf", "application/pdf", "%PDF-1.4 test")); err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("model called %d times, want 1", len(*bodies))
	}
	messages, _ := (*bodies)[0]["messages"].([]any)
	last, _ := messages[len(messages)-1].(map[string]any)
	parts, _ := last["content"].([]any)
	var file map[string]any
	for _, p := range parts {
		if part, _ := p.(map[string]any); part["type"] == "file" {
			file, _ = part["file"].(map[string]any)
		}
	}
	data, _ := file["file_data"].(string)
	if file["filename"] != "report.pdf" || !strings.HasPrefix(data, "data:application/pdf;base64,") {
		t.Fatalf("user message = %#v, want a file part for report.pdf", last)
	}
}

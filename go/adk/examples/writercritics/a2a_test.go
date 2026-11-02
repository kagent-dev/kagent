package writercritics

import (
	"fmt"
	"os"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/server/adka2a/v2"

	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
)

// authorKey is where the ADK a2a executor records which agent wrote an event.
const authorKey = "adk_author"

// artifactFrame is what a chat client receives for one artifact update.
type artifactFrame struct {
	id     string
	append bool
	author string
	text   string
}

// frames runs the workflow through the real ADK a2a executor, as the BYO runtime
// does, and returns the artifact updates in order.
func frames(t *testing.T, mode adka2a.OutputMode) []artifactFrame {
	t.Helper()
	doc, err := os.ReadFile(documentPath)
	if err != nil {
		t.Fatal(err)
	}
	model := &scripted{approveFrom: map[string]int{"Clarity": 1, "Accuracy": 1, "Style": 3}}
	reg := declarative.NewRegistry()
	if err := reg.RegisterModel("reviewer", model); err != nil {
		t.Fatal(err)
	}
	spec, err := reg.Parse(doc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	system, err := declarative.Build(t.Context(), spec, reg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Cleanup(func() { _ = system.Close() })

	root := system.Root()
	executor := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig: runner.Config{AppName: root.Name(), Agent: root, SessionService: adksession.InMemoryService()},
		OutputMode:   mode,
	})
	reqCtx := &a2asrv.ExecutorContext{
		TaskID:    "task-1",
		ContextID: "context-1",
		Message:   a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("why the sky is blue")),
	}

	var out []artifactFrame
	for event, err := range executor.Execute(t.Context(), reqCtx) {
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		update, ok := event.(*a2atype.TaskArtifactUpdateEvent)
		if !ok || update.Artifact == nil {
			continue
		}
		var text strings.Builder
		for _, part := range update.Artifact.Parts {
			text.WriteString(part.Text())
		}
		author, _ := update.Artifact.Metadata[authorKey].(string)
		out = append(out, artifactFrame{id: string(update.Artifact.ID), append: update.Append, author: author, text: text.String()})
	}
	return out
}

func distinctIDs(frames []artifactFrame) int {
	seen := map[string]bool{}
	for _, f := range frames {
		seen[f.id] = true
	}
	return len(seen)
}

// The default mode is the bug the chat showed: every node's reply is appended to
// one artifact, and nothing says who wrote what.
func TestDefaultOutputModeMergesEveryReplyIntoOneArtifact(t *testing.T) {
	got := frames(t, adka2a.OutputArtifactPerRun)
	if len(got) < 5 {
		t.Fatalf("got %d frames, want the workflow's replies: %v", len(got), got)
	}
	if n := distinctIDs(got); n != 1 {
		t.Fatalf("default mode used %d artifacts, the bug needs exactly 1: %v", n, got)
	}
	for _, f := range got {
		if f.author != "" {
			t.Fatalf("default mode unexpectedly carries an author: %v", got)
		}
	}
}

// One artifact per event keeps replies apart and names the sender of each.
func TestPerEventOutputModeSeparatesRepliesAndNamesTheSender(t *testing.T) {
	got := frames(t, adka2a.OutputArtifactPerEvent)
	if n := distinctIDs(got); n != len(got) {
		t.Fatalf("%d frames share %d artifacts, want one artifact per reply: %v", len(got), n, got)
	}
	for _, f := range got {
		if f.append {
			t.Fatalf("a reply was appended to an earlier artifact: %v", f)
		}
		if f.author == "" {
			t.Fatalf("a reply has no author: %v", f)
		}
	}

	byAuthor := map[string]int{}
	for _, f := range got {
		byAuthor[f.author]++
	}
	// Three turns: the writer drafts three times and every critic reviews three times.
	want := map[string]int{"writer": 3, "critic_clarity": 3, "critic_accuracy": 3, "critic_style": 3}
	for author, n := range want {
		if byAuthor[author] != n {
			t.Fatalf("author %q wrote %d replies, want %d; all: %s", author, byAuthor[author], n, fmt.Sprint(byAuthor))
		}
	}
}

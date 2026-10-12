package a2a

import (
	"context"
	"iter"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/turn"
	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/workspace"
	adksession "google.golang.org/adk/v2/session"
)

// directoryRecorder is the upstream executor of a turn that records the
// directory its tools work in, then works and completes.
type directoryRecorder struct {
	directory string
	ran       bool
}

func (e *directoryRecorder) Execute(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		e.ran, e.directory = true, turn.Directory(ctx)
		if !yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, nil), nil) {
			return
		}
		yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCompleted,
			a2atype.NewMessageForTask(a2atype.MessageRoleAgent, reqCtx, a2atype.NewTextPart("answered"))), nil)
	}
}

func (e *directoryRecorder) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(func(a2atype.Event, error) bool) {}
}

// workspaceForge is a workspace volume on the local filesystem: an origin and
// a bare mirror of each repository, and an empty session directory.
type workspaceForge struct {
	origins, mirrors, directory string
}

func newWorkspaceForge(t *testing.T, repositories ...string) *workspaceForge {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	// The setup's git sees the turn's environment, HOME included.
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	f := &workspaceForge{origins: filepath.Join(root, "origins"), mirrors: filepath.Join(root, "mirrors"), directory: filepath.Join(root, "sessions", "s")}
	if err := os.MkdirAll(f.directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, repository := range repositories {
		origin := filepath.Join(f.origins, repository)
		runGit(t, "", "init", "--quiet", "--initial-branch=main", origin)
		if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("# "+repository+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, origin, "add", ".")
		runGit(t, origin, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--quiet", "--message", "init")
		runGit(t, "", "clone", "--quiet", "--mirror", origin, filepath.Join(f.mirrors, repository+".git"))
	}
	return f
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

// dispatched is the context of a turn whose dispatch carries headers.
func dispatched(t *testing.T, headers map[string][]string) context.Context {
	ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(headers))
	return ctx
}

func (f *workspaceForge) headers(changed string) map[string][]string {
	return map[string][]string{
		kagenta2a.WorkingDirectoryHeader:    {f.directory},
		kagenta2a.MirrorsDirectoryHeader:    {f.mirrors},
		kagenta2a.ChangedRepositoriesHeader: {changed},
	}
}

// runTurn executes one turn and returns its status updates.
func runTurn(t *testing.T, executor *KAgentExecutor, ctx context.Context, reqCtx *a2asrv.ExecutorContext) []*a2atype.TaskStatusUpdateEvent {
	t.Helper()
	var updates []*a2atype.TaskStatusUpdateEvent
	for event, err := range executor.Execute(ctx, reqCtx) {
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok {
			updates = append(updates, update)
		}
	}
	return updates
}

func statusText(update *a2atype.TaskStatusUpdateEvent) string {
	if update.Status.Message == nil {
		return ""
	}
	var text strings.Builder
	for _, part := range update.Status.Message.Parts {
		text.WriteString(part.Text())
	}
	return text.String()
}

func newTurn() *a2asrv.ExecutorContext {
	return &a2asrv.ExecutorContext{
		TaskID: "task-1", ContextID: "ctx-1",
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("work in the workspace")),
	}
}

// A session without a volume sets nothing up: a dispatch without mirrors
// moves the tools to its directory only, and one without a directory leaves
// them in the scratch directory.
func TestKAgentExecutor_SessionWithoutVolumeSetsNothingUp(t *testing.T) {
	durable := filepath.Join(t.TempDir(), "data", "workspace")
	for name, test := range map[string]struct {
		headers map[string][]string
		want    string
	}{
		"durable directory": {headers: map[string][]string{kagenta2a.WorkingDirectoryHeader: {durable}, kagenta2a.ChangedRepositoriesHeader: {"e2e/alpha"}}, want: durable},
		"no dispatch":       {headers: map[string][]string{}},
	} {
		t.Run(name, func(t *testing.T) {
			builtin := &directoryRecorder{}
			executor := &KAgentExecutor{builtin: builtin, logger: slog.New(slog.DiscardHandler)}
			updates := runTurn(t, executor, dispatched(t, test.headers), newTurn())
			if builtin.directory != test.want {
				t.Errorf("tools work in %q, want %q", builtin.directory, test.want)
			}
			for _, update := range updates {
				if update.Status.State == a2atype.TaskStateWorking && update.Status.Message != nil {
					t.Errorf("unexpected status message %q", statusText(update))
				}
			}
			if test.want == "" {
				return
			}
			entries, err := os.ReadDir(test.want)
			if err != nil || len(entries) != 0 {
				t.Errorf("the directory holds %v (%v), want it created and empty", entries, err)
			}
		})
	}
}

// A fetch the origin refuses is the turn's first status message, naming the
// repository and the reason; the rest of the directory is set up and the turn
// answers.
func TestKAgentExecutor_RefusedFetchIsTheFirstStatusMessage(t *testing.T) {
	f := newWorkspaceForge(t, "e2e/alpha", "e2e/beta")
	if err := os.RemoveAll(filepath.Join(f.origins, "e2e/beta")); err != nil {
		t.Fatal(err)
	}
	builtin := &directoryRecorder{}
	executor := &KAgentExecutor{builtin: builtin, logger: slog.New(slog.DiscardHandler)}
	updates := runTurn(t, executor, dispatched(t, f.headers("e2e/beta")), newTurn())

	if builtin.directory != f.directory {
		t.Errorf("tools work in %q, want %q", builtin.directory, f.directory)
	}
	if len(updates) != 2 || updates[0].Status.State != a2atype.TaskStateWorking || updates[1].Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("status updates = %v, want working then completed", updates)
	}
	if first := statusText(updates[0]); !strings.Contains(first, "e2e/beta: the origin refused the fetch") {
		t.Errorf("first status message = %q, want the refused fetch", first)
	}
	if statusText(updates[1]) != "answered" {
		t.Errorf("the turn answered %q", statusText(updates[1]))
	}
	for _, repository := range []string{"e2e/alpha", "e2e/beta"} {
		if _, err := os.Stat(filepath.Join(f.directory, repository, ".git", "objects", "info", "alternates")); err != nil {
			t.Errorf("%s is not cloned with alternates: %v", repository, err)
		}
	}
	notes, err := os.ReadFile(filepath.Join(f.directory, workspaceNotes))
	if err != nil || !strings.Contains(string(notes), "git switch") {
		t.Errorf("the notes = %q (%v), want how to check a repository out", notes, err)
	}
}

// A turn that resumes a task waiting for input continues the turn that set
// the directory up: it clones and fetches nothing.
func TestKAgentExecutor_ResumedTurnSetsNothingUp(t *testing.T) {
	f := newWorkspaceForge(t, "e2e/alpha")
	builtin := &directoryRecorder{}
	executor := &KAgentExecutor{builtin: builtin, logger: slog.New(slog.DiscardHandler)}
	reqCtx := newTurn()
	reqCtx.StoredTask = &a2atype.Task{ID: "task-1", ContextID: "ctx-1", Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired}}
	runTurn(t, executor, dispatched(t, f.headers("e2e/alpha")), reqCtx)

	if builtin.directory != f.directory {
		t.Errorf("tools work in %q, want %q", builtin.directory, f.directory)
	}
	entries, err := os.ReadDir(f.directory)
	if err != nil || len(entries) != 0 {
		t.Errorf("the resumed turn set the directory up: %v (%v)", entries, err)
	}
}

// A returning session whose directory is set up again, such as after the
// volume's owner cleaned it up, is told so in its first status message; a
// later turn sets up nothing and says nothing.
func TestKAgentExecutor_ReturningSessionIsToldOfAFreshDirectory(t *testing.T) {
	f := newWorkspaceForge(t, "e2e/alpha")
	sessions := adksession.InMemoryService()
	const appName = "app"
	created, err := sessions.Create(t.Context(), &adksession.CreateRequest{AppName: appName, UserID: "A2A_USER_ctx-1", SessionID: "ctx-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.AppendEvent(t.Context(), created.Session, adksession.NewEvent(t.Context(), "earlier-turn")); err != nil {
		t.Fatal(err)
	}
	executor := &KAgentExecutor{builtin: &directoryRecorder{}, sessionService: sessions, appName: appName, logger: slog.New(slog.DiscardHandler)}

	updates := runTurn(t, executor, dispatched(t, f.headers("")), newTurn())
	if first := statusText(updates[0]); !strings.Contains(first, "set up again from the workspace's mirrors") {
		t.Errorf("first status message = %q, want the fresh directory", first)
	}
	if _, err := os.Stat(filepath.Join(f.directory, workspace.StateFile)); err != nil {
		t.Errorf("the setup is not recorded: %v", err)
	}
	updates = runTurn(t, executor, dispatched(t, f.headers("")), newTurn())
	if text := statusText(updates[0]); text != "" {
		t.Errorf("a later turn's first status message = %q, want none", text)
	}
}

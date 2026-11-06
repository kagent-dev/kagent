package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"

	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
)

func TestDispatchFrom(t *testing.T) {
	require.Equal(t, Dispatch{}, DispatchFrom(t.Context()), "a call without a dispatch")
	ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{
		kagenta2a.WorkingDirectoryHeader:    {"/workspace"},
		kagenta2a.MirrorsDirectoryHeader:    {"/mirrors"},
		kagenta2a.ChangedRepositoriesHeader: {"e2e/alpha, e2e/beta", "e2e/beta,../escape,e2e,a/b/c,", "e2e/gamma"},
	}))
	require.Equal(t, Dispatch{
		Directory: "/workspace", Mirrors: "/mirrors",
		Changed: []string{"e2e/alpha", "e2e/beta", "e2e/gamma"},
		Ignored: []string{"../escape", "e2e", "a/b/c"},
	}, DispatchFrom(ctx))
}

func TestWithoutUserinfo(t *testing.T) {
	for origin, want := range map[string]string{
		"https://x-access-token:secret@github.com/o/r.git": "https://github.com/o/r.git",
		"https://github.com/o/r.git":                       "https://github.com/o/r.git",
		"/srv/git/o/r.git":                                 "/srv/git/o/r.git",
		"git@github.com:o/r.git":                           "git@github.com:o/r.git",
	} {
		require.Equal(t, want, withoutUserinfo(origin), origin)
	}
}

func TestReportNotice(t *testing.T) {
	require.Empty(t, Report{Fetched: []string{"o/r"}}.Notice())
	notice := Report{Problems: []Problem{{Repository: "o/r", Reason: "the fetch from origin failed: 403"}}}.Notice()
	require.Contains(t, notice, "o/r: the fetch from origin failed: 403")

	// A retry names what it brought up to date, and what failed again as a
	// problem only.
	retried := Report{Retried: []string{"o/r", "o/s"}, Problems: []Problem{{Repository: "o/s", Reason: "not cloned: disk full"}}}.Notice()
	require.Equal(t, "Workspace setup: an earlier turn could not clone or fetch these repositories; they are up to date now: o/r.\n"+
		"Workspace setup: these repositories stay at the workspace's last sync.\n- o/s: not cloned: disk full", retried)
}

// A turn's notice names a failed setup, the directory a returning session
// finds set up again, and the problems, in that order; a first-ever setup and
// a directory set up before say nothing.
func TestReportTurnNotice(t *testing.T) {
	problems := Report{SetUp: true, Problems: []Problem{{Repository: "o/r", Reason: "the fetch from origin failed: 403"}}}
	for _, test := range []struct {
		name      string
		report    Report
		err       error
		returning bool
		want      []string
	}{
		{name: "set up before", report: Report{}, returning: true},
		{name: "first-ever setup", report: Report{SetUp: true}},
		{name: "returning session", report: Report{SetUp: true}, returning: true, want: []string{FreshDirectoryNotice}},
		{
			name: "returning session with a problem", report: problems, err: errors.New("disk full"), returning: true,
			want: []string{"Workspace setup failed: disk full", FreshDirectoryNotice, problems.Notice()},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, strings.Join(test.want, "\n"), test.report.TurnNotice(test.err, test.returning))
		})
	}
}

// forge is a set of origins on the local filesystem and a workspace volume
// with a bare mirror of each.
type forge struct {
	origins, mirrors, directory string
}

func newForge(t *testing.T, repositories ...string) *forge {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	f := &forge{origins: filepath.Join(root, "origins"), mirrors: filepath.Join(root, "mirrors"), directory: filepath.Join(root, "sessions", "s")}
	require.NoError(t, os.MkdirAll(f.directory, 0o755))
	for _, repository := range repositories {
		origin := f.origin(repository)
		gitIn(t, "", "init", "--quiet", "--initial-branch=main", origin)
		f.commit(t, repository, "README.md", "# "+repository+"\n")
		// A file:// URL, as a provider's origin is a URL: a mirror's origin is
		// never a path on the local filesystem.
		gitIn(t, "", "clone", "--quiet", "--mirror", f.originURL(repository), filepath.Join(f.mirrors, repository+".git"))
	}
	return f
}

func (f *forge) origin(repository string) string {
	return filepath.Join(f.origins, filepath.FromSlash(repository))
}

func (f *forge) originURL(repository string) string {
	return "file://" + f.origin(repository)
}

func (f *forge) commit(t *testing.T, repository, name, content string) string {
	t.Helper()
	origin := f.origin(repository)
	require.NoError(t, os.WriteFile(filepath.Join(origin, name), []byte(content), 0o644))
	gitIn(t, origin, "add", ".")
	gitIn(t, origin, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--quiet", "--message", "Change "+name)
	return gitIn(t, origin, "rev-parse", "HEAD")
}

func (f *forge) dispatch(changed ...string) Dispatch {
	return Dispatch{Directory: f.directory, Mirrors: f.mirrors, Changed: changed}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), output)
	return strings.TrimSpace(string(output))
}

// Every mirror gets a clone with alternates and no working tree, whose origin
// is the mirror's; the changed repository alone is fetched; the notes say how
// to check one out, and a switch of the default branch does.
func TestPrepare(t *testing.T) {
	f := newForge(t, "e2e/alpha", "e2e/beta")
	synced := gitIn(t, f.origin("e2e/alpha"), "rev-parse", "HEAD")
	moved := f.commit(t, "e2e/beta", "CHANGED.md", "after the sync\n")
	// An origin that moved but was not named changed is never fetched.
	f.commit(t, "e2e/alpha", "UNNAMED.md", "not named changed\n")

	report, err := Prepare(t.Context(), f.dispatch("e2e/beta"), Options{Notes: "NOTES.md"})
	require.NoError(t, err)
	require.Equal(t, Report{SetUp: true, Cloned: []string{"e2e/alpha", "e2e/beta"}, Fetched: []string{"e2e/beta"}}, report)
	require.Empty(t, report.Notice())

	for _, repository := range []string{"e2e/alpha", "e2e/beta"} {
		clone := filepath.Join(f.directory, repository)
		alternates, err := os.ReadFile(filepath.Join(clone, ".git", "objects", "info", "alternates"))
		require.NoError(t, err)
		require.Equal(t, filepath.Join(f.mirrors, repository+".git", "objects"), strings.TrimSpace(string(alternates)))
		require.Equal(t, f.originURL(repository), gitIn(t, clone, "remote", "get-url", "origin"))
		entries, err := os.ReadDir(clone)
		require.NoError(t, err)
		require.Len(t, entries, 1, "%s has no working tree", repository)
		require.Equal(t, "refs/heads/main", gitIn(t, clone, "symbolic-ref", "HEAD"))
	}
	require.Equal(t, synced, gitIn(t, filepath.Join(f.directory, "e2e/alpha"), "rev-parse", "origin/main"))
	require.Equal(t, moved, gitIn(t, filepath.Join(f.directory, "e2e/beta"), "rev-parse", "origin/main"))

	beta := filepath.Join(f.directory, "e2e/beta")
	gitIn(t, beta, "switch", "--quiet", "main")
	require.FileExists(t, filepath.Join(beta, "CHANGED.md"))
	require.Equal(t, moved, gitIn(t, beta, "rev-parse", "HEAD"))

	notes, err := os.ReadFile(filepath.Join(f.directory, "NOTES.md"))
	require.NoError(t, err)
	require.Contains(t, string(notes), "git switch <default branch>")
	require.Contains(t, string(notes), "`e2e/alpha`")

	var recorded state
	contents, err := os.ReadFile(filepath.Join(f.directory, StateFile))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(contents, &recorded))
	require.Equal(t, []string{"e2e/beta"}, recorded.Fetched)
}

// A directory set up once is left alone: a later turn neither clones nor
// fetches again.
func TestPrepareOnce(t *testing.T) {
	f := newForge(t, "e2e/alpha")
	first, err := Prepare(t.Context(), f.dispatch(), Options{})
	require.NoError(t, err)
	require.True(t, first.SetUp)
	f.commit(t, "e2e/alpha", "LATER.md", "later\n")
	second, err := Prepare(t.Context(), f.dispatch("e2e/alpha"), Options{})
	require.NoError(t, err)
	require.Equal(t, Report{}, second)
	require.NotEqual(t, gitIn(t, f.origin("e2e/alpha"), "rev-parse", "HEAD"),
		gitIn(t, filepath.Join(f.directory, "e2e/alpha"), "rev-parse", "origin/main"))
}

// A fetch that failed is retried by the next turn, which says so; once it
// succeeds, the directory is left alone.
func TestPrepareRetriesAFailedFetch(t *testing.T) {
	f := newForge(t, "e2e/alpha", "e2e/beta")
	moved := f.commit(t, "e2e/beta", "CHANGED.md", "after the sync\n")
	aside := f.origin("e2e/beta") + ".aside"
	require.NoError(t, os.Rename(f.origin("e2e/beta"), aside))
	first, err := Prepare(t.Context(), f.dispatch("e2e/beta"), Options{})
	require.NoError(t, err)
	require.Empty(t, first.Fetched)
	require.Len(t, first.Problems, 1)
	require.Equal(t, "e2e/beta", first.Problems[0].Repository)

	// Still failing: the next turn tries again and the problem stays.
	again, err := Prepare(t.Context(), f.dispatch("e2e/beta"), Options{})
	require.NoError(t, err)
	require.Equal(t, []string{"e2e/beta"}, again.Retried)
	require.Len(t, again.Problems, 1)
	require.False(t, again.SetUp)

	require.NoError(t, os.Rename(aside, f.origin("e2e/beta")))
	retried, err := Prepare(t.Context(), f.dispatch("e2e/beta"), Options{})
	require.NoError(t, err)
	require.Equal(t, Report{Fetched: []string{"e2e/beta"}, Retried: []string{"e2e/beta"}}, retried)
	require.Contains(t, retried.TurnNotice(nil, true), "they are up to date now: e2e/beta.")
	require.NotContains(t, retried.TurnNotice(nil, true), FreshDirectoryNotice, "a retry is no fresh directory")
	require.Equal(t, moved, gitIn(t, filepath.Join(f.directory, "e2e/beta"), "rev-parse", "origin/main"))
	recorded := readTestState(t, f.directory)
	require.Equal(t, []string{"e2e/alpha", "e2e/beta"}, recorded.Cloned)
	require.Equal(t, []string{"e2e/beta"}, recorded.Fetched)
	require.Empty(t, recorded.Problems)

	settled, err := Prepare(t.Context(), f.dispatch("e2e/beta"), Options{})
	require.NoError(t, err)
	require.Equal(t, Report{}, settled)
}

// A clone that failed is cloned by the next turn, fetched when it is changed,
// and joins the notes.
func TestPrepareRetriesAFailedClone(t *testing.T) {
	f := newForge(t, "e2e/alpha", "g/p")
	gitIn(t, filepath.Join(f.mirrors, "g/p.git"), "remote", "set-url", "origin", "https://gitlab.example.com/g/p.git")
	moved := f.commit(t, "e2e/alpha", "CHANGED.md", "after the sync\n")
	// A file where the clone goes fails the clone.
	require.NoError(t, os.MkdirAll(filepath.Join(f.directory, "e2e"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.directory, "e2e/alpha"), []byte("in the way"), 0o644))
	first, err := Prepare(t.Context(), f.dispatch("e2e/alpha"), Options{Notes: "NOTES.md"})
	require.NoError(t, err)
	require.Equal(t, []string{"g/p"}, first.Cloned)
	require.Len(t, first.Problems, 1)
	require.Equal(t, "e2e/alpha", first.Problems[0].Repository)
	require.Contains(t, first.Problems[0].Reason, "not cloned")

	require.NoError(t, os.Remove(filepath.Join(f.directory, "e2e/alpha")))
	// The changed repositories are the dispatch's, as on every turn.
	retried, err := Prepare(t.Context(), f.dispatch("e2e/alpha"), Options{Notes: "NOTES.md"})
	require.NoError(t, err)
	require.Equal(t, Report{Cloned: []string{"e2e/alpha"}, Fetched: []string{"e2e/alpha"}, Retried: []string{"e2e/alpha"}}, retried)
	// The local origin of e2e/alpha is a path: fetched over file://.
	require.Equal(t, moved, gitIn(t, filepath.Join(f.directory, "e2e/alpha"), "rev-parse", "origin/main"))
	notes, err := os.ReadFile(filepath.Join(f.directory, "NOTES.md"))
	require.NoError(t, err)
	require.Contains(t, string(notes), "`e2e/alpha`")
	require.Contains(t, string(notes), "`g/p`")
	recorded := readTestState(t, f.directory)
	require.Equal(t, []string{"g/p", "e2e/alpha"}, recorded.Cloned)
	require.Empty(t, recorded.Problems)
}

// The state a setup recorded before retries existed is the same file: its
// problems are retried by the next turn.
func TestPrepareRetriesAnEarlierState(t *testing.T) {
	f := newForge(t, "e2e/alpha")
	_, err := Prepare(t.Context(), f.dispatch(), Options{})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(f.directory, StateFile), []byte(`{"setUp":"2026-10-01T00:00:00Z","cloned":["e2e/alpha"],`+
		`"problems":[{"repository":"e2e/alpha","reason":"the fetch from origin failed: fatal: unable to access"}]}`), 0o644))
	moved := f.commit(t, "e2e/alpha", "LATER.md", "later\n")
	report, err := Prepare(t.Context(), f.dispatch("e2e/alpha"), Options{})
	require.NoError(t, err)
	require.Equal(t, Report{Fetched: []string{"e2e/alpha"}, Retried: []string{"e2e/alpha"}}, report)
	require.Equal(t, moved, gitIn(t, filepath.Join(f.directory, "e2e/alpha"), "rev-parse", "origin/main"))
	recorded := readTestState(t, f.directory)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), recorded.SetUp, "a retry keeps when the directory was set up")
	require.Empty(t, recorded.Problems)
}

// What no retry can fix, a changed repository without a mirror or a changed
// entry that names no <owner>/<repository>, is reported once and not retried.
func TestPrepareDoesNotRetryWithoutAMirror(t *testing.T) {
	f := newForge(t, "e2e/alpha")
	dispatch := f.dispatch("e2e/gone")
	dispatch.Ignored = []string{"group/subgroup/api"}
	first, err := Prepare(t.Context(), dispatch, Options{})
	require.NoError(t, err)
	require.Equal(t, []Problem{
		{Repository: "group/subgroup/api", Reason: "not fetched: it names no <owner>/<repository>"},
		{Repository: "e2e/gone", Reason: "not fetched: the workspace has no mirror of it"},
	}, first.Problems)
	require.Contains(t, first.Notice(), "- group/subgroup/api: not fetched: it names no <owner>/<repository>")

	second, err := Prepare(t.Context(), dispatch, Options{})
	require.NoError(t, err)
	require.Equal(t, Report{}, second)
	require.Len(t, readTestState(t, f.directory).Problems, 2, "the state keeps what was never set up")
}

// A state the agent overwrote with something else records a setup with
// nothing to retry, as any state did before retries.
func TestPrepareWithAnUnreadableState(t *testing.T) {
	f := newForge(t, "e2e/alpha")
	require.NoError(t, os.MkdirAll(filepath.Join(f.directory, filepath.Dir(StateFile)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.directory, StateFile), []byte("not json"), 0o644))
	report, err := Prepare(t.Context(), f.dispatch("e2e/alpha"), Options{})
	require.NoError(t, err)
	require.Equal(t, Report{}, report)
	require.NoDirExists(t, filepath.Join(f.directory, "e2e/alpha"))
}

func readTestState(t *testing.T, directory string) state {
	t.Helper()
	var recorded state
	contents, err := os.ReadFile(filepath.Join(directory, StateFile))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(contents, &recorded))
	return recorded
}

// A fetch the origin refuses, and a changed repository without a mirror, are
// problems of the report; the rest of the directory is set up.
func TestPrepareReportsARefusedFetch(t *testing.T) {
	f := newForge(t, "e2e/alpha", "e2e/beta")
	require.NoError(t, os.RemoveAll(f.origin("e2e/beta")))
	report, err := Prepare(t.Context(), f.dispatch("e2e/beta", "e2e/gone"), Options{})
	require.NoError(t, err)
	require.Equal(t, []string{"e2e/alpha", "e2e/beta"}, report.Cloned)
	require.Empty(t, report.Fetched)
	require.Len(t, report.Problems, 2)
	require.Equal(t, "e2e/beta", report.Problems[0].Repository)
	require.Contains(t, report.Problems[0].Reason, "the fetch from origin failed")
	require.Equal(t, Problem{Repository: "e2e/gone", Reason: "not fetched: the workspace has no mirror of it"}, report.Problems[1])
	require.Contains(t, report.Notice(), "e2e/beta: the fetch from origin failed")
}

// unreadableMirrorConfig is what git prints when the mirror's config cannot
// be read, as when the workspace's sync replaces it mid-read.
const unreadableMirrorConfig = "warning: unable to access './config': Permission denied\nfatal: unable to access 'config': Permission denied"

// failOriginReads puts a git on PATH that fails the next failures reads of a
// mirror's origin as an unreadable config does, and logs every command it
// runs to the returned file. Root reads any file, so the failure is
// simulated rather than made with a mode.
func failOriginReads(t *testing.T, failures int) string {
	t.Helper()
	real, err := exec.LookPath("git")
	require.NoError(t, err)
	bin := t.TempDir()
	log, counter, stderr := filepath.Join(bin, "log"), filepath.Join(bin, "failures"), filepath.Join(bin, "stderr")
	require.NoError(t, os.WriteFile(counter, []byte(strconv.Itoa(failures)), 0o644))
	require.NoError(t, os.WriteFile(stderr, []byte(unreadableMirrorConfig+"\n"), 0o644))
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %[1]q
case "$*" in *"config --get remote.origin.url"*)
	left=$(cat %[2]q)
	if [ "$left" -gt 0 ]; then
		echo $((left - 1)) > %[2]q
		cat %[3]q >&2
		exit 128
	fi;;
esac
exec %[4]q "$@"
`, log, counter, stderr, real)
	require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// A mirror's origin that cannot be read is a problem naming the cause: the
// clone stands without an origin, never with the mirror's path, and is not
// fetched, from the mirror or anywhere, until a later turn reads it.
func TestPrepareReportsAnUnreadableMirrorOrigin(t *testing.T) {
	f := newForge(t, "e2e/alpha")
	f.commit(t, "e2e/alpha", "CHANGED.md", "after the sync\n")
	log := failOriginReads(t, originReadAttempts)
	report, err := Prepare(t.Context(), f.dispatch("e2e/alpha"), Options{Environment: os.Environ()})
	require.NoError(t, err)
	require.Equal(t, []string{"e2e/alpha"}, report.Cloned)
	require.Empty(t, report.Fetched)
	require.Equal(t, []Problem{{Repository: "e2e/alpha", Reason: "origin unknown: fatal: unable to access 'config': Permission denied"}}, report.Problems)
	commands, err := os.ReadFile(log)
	require.NoError(t, err)
	require.NotContains(t, string(commands), "fetch", "nothing is fetched")
	require.Equal(t, originReadAttempts, strings.Count(string(commands), "config --get remote.origin.url"), "a failed read is tried again")
	command := exec.CommandContext(t.Context(), "git", "config", "--get", "remote.origin.url")
	command.Dir = filepath.Join(f.directory, "e2e/alpha")
	require.Error(t, command.Run(), "the clone keeps no origin")

	// The next turn reads the origin again, sets it on the clone in place and
	// fetches the changed repository.
	moved := gitIn(t, f.origin("e2e/alpha"), "rev-parse", "HEAD")
	retried, err := Prepare(t.Context(), f.dispatch("e2e/alpha"), Options{Environment: os.Environ()})
	require.NoError(t, err)
	require.Equal(t, Report{Fetched: []string{"e2e/alpha"}, Retried: []string{"e2e/alpha"}}, retried)
	clone := filepath.Join(f.directory, "e2e/alpha")
	require.Equal(t, f.originURL("e2e/alpha"), gitIn(t, clone, "remote", "get-url", "origin"))
	require.Equal(t, moved, gitIn(t, clone, "rev-parse", "origin/main"))
	require.Empty(t, readTestState(t, f.directory).Problems)
}

// A read of the mirror's origin that fails once, then succeeds, gives the
// clone the mirror's origin and fetches it.
func TestPrepareRetriesAMirrorOriginRead(t *testing.T) {
	f := newForge(t, "e2e/alpha")
	moved := f.commit(t, "e2e/alpha", "CHANGED.md", "after the sync\n")
	failOriginReads(t, 1)
	report, err := Prepare(t.Context(), f.dispatch("e2e/alpha"), Options{Environment: os.Environ()})
	require.NoError(t, err)
	require.Equal(t, Report{SetUp: true, Cloned: []string{"e2e/alpha"}, Fetched: []string{"e2e/alpha"}}, report)
	clone := filepath.Join(f.directory, "e2e/alpha")
	require.Equal(t, f.originURL("e2e/alpha"), gitIn(t, clone, "remote", "get-url", "origin"))
	require.Equal(t, moved, gitIn(t, clone, "rev-parse", "origin/main"))
}

// A mirror without an origin stays the clone's origin, and a changed
// repository behind it is not fetched: a fetch from the mirror moves nothing.
func TestPrepareWithAMirrorWithoutOrigin(t *testing.T) {
	f := newForge(t, "e2e/alpha")
	mirror := filepath.Join(f.mirrors, "e2e/alpha.git")
	gitIn(t, mirror, "remote", "remove", "origin")
	report, err := Prepare(t.Context(), f.dispatch("e2e/alpha"), Options{})
	require.NoError(t, err)
	require.Equal(t, []string{"e2e/alpha"}, report.Cloned)
	require.Empty(t, report.Fetched)
	require.Equal(t, []Problem{{Repository: "e2e/alpha", Reason: "not fetched: no provider origin"}}, report.Problems)
	require.Equal(t, mirror, gitIn(t, filepath.Join(f.directory, "e2e/alpha"), "remote", "get-url", "origin"))
}

func TestProviderOrigin(t *testing.T) {
	for origin, want := range map[string]bool{
		"https://github.com/o/r.git": true,
		"git@github.com:o/r.git":     true,
		"ssh://git@host/o/r.git":     true,
		"file:///srv/git/o/r.git":    true,
		"/mirrors/o/r.git":           false,
		"../o/r.git":                 false,
		"./a:b":                      false,
		"":                           false,
	} {
		require.Equal(t, want, providerOrigin(origin), origin)
	}
}

// A reason keeps the first fatal: or error: line, which names the cause,
// beside the last.
func TestReason(t *testing.T) {
	exit := errors.New("exit status 128")
	for _, test := range []struct{ name, stderr, want string }{
		{name: "no output", want: "exit status 128"},
		{name: "one line", stderr: "fatal: repository not found\n", want: "fatal: repository not found"},
		{
			name:   "the cause before where git stopped",
			stderr: unreadableMirrorConfig + "\nfatal: expected flush after capabilities\n",
			want:   "fatal: unable to access 'config': Permission denied; fatal: expected flush after capabilities",
		},
		{name: "no fatal line", stderr: "warning: one\nhint: two", want: "hint: two"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, reason(test.stderr, exit))
		})
	}
	require.Len(t, reason("fatal: "+strings.Repeat("x", 2*maxReasonBytes), exit), maxReasonBytes)
}

// A fetch beyond its timeout is a problem, not a hung turn.
func TestPrepareBoundsAFetch(t *testing.T) {
	f := newForge(t, "e2e/alpha")
	// An origin that never answers: a fetch over ext:: from a command that
	// sleeps.
	clone := filepath.Join(f.mirrors, "e2e/alpha.git")
	gitIn(t, clone, "config", "remote.origin.url", "ext::sleep 30")
	t.Setenv("GIT_ALLOW_PROTOCOL", "ext:file")
	started := time.Now()
	report, err := Prepare(t.Context(), f.dispatch("e2e/alpha"), Options{Environment: os.Environ(), FetchTimeout: 200 * time.Millisecond})
	require.NoError(t, err)
	require.Less(t, time.Since(started), 20*time.Second)
	require.Len(t, report.Problems, 1)
	require.Contains(t, report.Problems[0].Reason, "timed out")
}

// A link the agent left in its directory never takes a write of the setup
// outside it.
func TestPrepareStaysInTheDirectory(t *testing.T) {
	f := newForge(t, "e2e/alpha")
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("untouched"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(f.directory, "NOTES.md")))
	require.NoError(t, os.Symlink(filepath.Dir(outside), filepath.Join(f.directory, ".kagent")))
	_, err := Prepare(t.Context(), f.dispatch(), Options{Notes: "NOTES.md"})
	require.Error(t, err)
	contents, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "untouched", string(contents))
	_, err = os.Stat(filepath.Join(filepath.Dir(outside), "workspace.json"))
	require.True(t, errors.Is(err, os.ErrNotExist), "the state was written outside the directory")
}

// A dispatch without mirrors has nothing to set up, and writes nothing.
func TestPrepareWithoutMirrors(t *testing.T) {
	directory := t.TempDir()
	report, err := Prepare(t.Context(), Dispatch{Directory: directory, Changed: []string{"o/r"}}, Options{Notes: "NOTES.md"})
	require.NoError(t, err)
	require.Equal(t, Report{}, report)
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Empty(t, entries)
	_, err = os.Stat(filepath.Join(directory, StateFile))
	require.True(t, errors.Is(err, os.ErrNotExist))
}

// The agent's own git clones a mirror that belongs to another user once its
// environment trusts the mirrors, and still refuses another user's
// repository outside them; a dispatch without mirrors trusts nothing.
func TestTrustMirrors(t *testing.T) {
	require.Equal(t, []string{"A=1"}, TrustMirrors(Dispatch{Directory: "/workspace"}, []string{"A=1"}))
	require.Equal(t, []string{"A=1", "GIT_CONFIG_KEY_0=x.y", "GIT_CONFIG_VALUE_0=z",
		"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_1=safe.directory", "GIT_CONFIG_VALUE_1=/mirrors/*"},
		TrustMirrors(Dispatch{Mirrors: "/mirrors"}, []string{"A=1", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=x.y", "GIT_CONFIG_VALUE_0=z"}),
		"an entry already configured stays")

	f := newForge(t, "o/r")
	elsewhere := filepath.Join(t.TempDir(), "elsewhere.git")
	gitIn(t, "", "clone", "--quiet", "--bare", f.origin("o/r"), elsewhere)
	// git's own switch for a repository that belongs to another user, and
	// its messages untranslated.
	environment := append(os.Environ(), "GIT_TEST_ASSUME_DIFFERENT_OWNER=1", "LC_ALL=C")
	clone := func(environment []string, source string) error {
		command := exec.CommandContext(t.Context(), "git", "clone", "--quiet", source, filepath.Join(t.TempDir(), "clone"))
		command.Env = environment
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, output)
		}
		return nil
	}
	mirror := filepath.Join(f.mirrors, "o", "r.git")
	require.ErrorContains(t, clone(environment, mirror), "dubious ownership", "an untrusted mirror")
	trusted := TrustMirrors(f.dispatch(), environment)
	require.NoError(t, clone(trusted, mirror))
	require.ErrorContains(t, clone(trusted, elsewhere), "dubious ownership", "a repository outside the mirrors")
}

func TestMirrors(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{"o/b.git", "o/a.git", "p/c.git", "o/not-a-mirror", ".hidden/x.git"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, path), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "o", "file.git"), nil, 0o644))
	repositories, err := Mirrors(dir)
	require.NoError(t, err)
	require.Equal(t, []string{"o/a", "o/b", "p/c"}, repositories)
}

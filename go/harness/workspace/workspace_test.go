package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/stretchr/testify/require"
)

// prepareRequestedEnv makes the test binary the harness binary PrepareAs
// runs: it answers the one request on its standard input, as the harness
// does for PrepareSubcommand.
const prepareRequestedEnv = "WORKSPACE_TEST_PREPARE_REQUESTED"

func TestMain(m *testing.M) {
	if os.Getenv(prepareRequestedEnv) == "1" {
		if err := PrepareRequested(context.Background(), os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

func TestDispatchFrom(t *testing.T) {
	require.Equal(t, Dispatch{}, DispatchFrom(t.Context()), "a call without a dispatch")
	ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{
		adk.WorkingDirectoryHeader:    {"/workspace"},
		adk.MirrorsDirectoryHeader:    {"/mirrors"},
		adk.ChangedRepositoriesHeader: {"e2e/alpha, e2e/beta", "e2e/beta,../escape,e2e,a/b/c,", "e2e/gamma"},
	}))
	require.Equal(t, Dispatch{
		Directory: "/workspace", Mirrors: "/mirrors",
		Changed: []string{"e2e/alpha", "e2e/beta", "e2e/gamma"},
	}, DispatchFrom(ctx))
}

func TestActorJWTHosts(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []string
		want   []string
	}{
		{name: "none"},
		{name: "one", values: []string{"muster.example.svc"}, want: []string{"muster.example.svc"}},
		{name: "joined and repeated, case folded", values: []string{"Muster.Example.svc, git.example.com", "muster.example.svc,,other.example.com"}, want: []string{"muster.example.svc", "git.example.com", "other.example.com"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			params := map[string][]string{}
			if test.values != nil {
				params[adk.ActorJWTHostsHeader] = test.values
			}
			ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(params))
			require.Equal(t, test.want, ActorJWTHosts(ctx))
		})
	}
	require.Nil(t, ActorJWTHosts(t.Context()), "a call outside A2A names no host")
}

func TestTakesActorJWT(t *testing.T) {
	hosts := []string{"muster.example.svc"}
	require.True(t, TakesActorJWT("https://Muster.Example.svc:8090/mcp", map[string]string{"X-Muster-Toolset": "all"}, hosts))
	require.False(t, TakesActorJWT("https://muster.example.svc/mcp", map[string]string{"authorization": "Bearer own"}, hosts), "a server that owns its Authorization")
	require.False(t, TakesActorJWT("https://tools.example.svc/mcp", nil, hosts), "another host")
	require.False(t, TakesActorJWT("https://muster.example.svc/mcp", nil, nil), "a turn without actor-JWT hosts")
	require.False(t, TakesActorJWT("://muster.example.svc", nil, hosts), "an unparsable URL")
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
	notice := Report{Problems: []Problem{{Repository: "o/r", Reason: "the origin refused the fetch: 403"}}}.Notice()
	require.Contains(t, notice, "o/r: the origin refused the fetch: 403")
}

// A turn's notice names a failed setup, the directory a returning session
// finds set up again, and the problems, in that order; a first-ever setup and
// a directory set up before say nothing.
func TestReportTurnNotice(t *testing.T) {
	problems := Report{SetUp: true, Problems: []Problem{{Repository: "o/r", Reason: "the origin refused the fetch: 403"}}}
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
		gitIn(t, "", "clone", "--quiet", "--mirror", origin, filepath.Join(f.mirrors, repository+".git"))
	}
	return f
}

func (f *forge) origin(repository string) string {
	return filepath.Join(f.origins, filepath.FromSlash(repository))
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
		require.Equal(t, f.origin(repository), gitIn(t, clone, "remote", "get-url", "origin"))
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

// A setup run as another process answers with the report Prepare gives, and
// with the error of a directory that cannot be set up at all.
func TestPrepareAs(t *testing.T) {
	f := newForge(t, "e2e/alpha", "e2e/beta")
	moved := f.commit(t, "e2e/beta", "CHANGED.md", "after the sync\n")
	environment := append(os.Environ(), prepareRequestedEnv+"=1")

	report, err := PrepareAs(t.Context(), f.dispatch("e2e/beta"), Options{Notes: "NOTES.md", Environment: environment}, nil, os.Args[0])
	require.NoError(t, err)
	require.Equal(t, Report{SetUp: true, Cloned: []string{"e2e/alpha", "e2e/beta"}, Fetched: []string{"e2e/beta"}}, report)
	require.Equal(t, moved, gitIn(t, filepath.Join(f.directory, "e2e/beta"), "rev-parse", "origin/main"))
	require.FileExists(t, filepath.Join(f.directory, "NOTES.md"))
	require.FileExists(t, filepath.Join(f.directory, StateFile))

	_, err = PrepareAs(t.Context(), Dispatch{Directory: "relative", Mirrors: f.mirrors}, Options{Environment: environment}, nil, os.Args[0])
	require.ErrorContains(t, err, "absolute paths")
	_, err = PrepareAs(t.Context(), f.dispatch(), Options{Environment: environment}, nil)
	require.ErrorContains(t, err, "command")
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
	require.Contains(t, report.Problems[0].Reason, "the origin refused the fetch")
	require.Equal(t, Problem{Repository: "e2e/gone", Reason: "not fetched: the workspace has no mirror of it"}, report.Problems[1])
	require.Contains(t, report.Notice(), "e2e/beta: the origin refused the fetch")
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

// A workspace turn's git answers a host's challenge with the placeholder, so
// the egress gateway has an Authorization header to replace, and gh gets its
// placeholder token; an own GH_TOKEN stands, and a turn without a working
// directory gets neither.
func TestWithCredentialPlaceholders(t *testing.T) {
	dispatch := Dispatch{Directory: t.TempDir()}
	environment := WithCredentialPlaceholders(dispatch, []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=safe.directory", "GIT_CONFIG_VALUE_0=/mirrors/*"})
	fill := exec.Command("git", "credential", "fill")
	fill.Env = append(environment, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	fill.Stdin = strings.NewReader("protocol=https\nhost=github.example.com\n\n")
	out, err := fill.Output()
	if err != nil {
		t.Fatalf("git credential fill: %v", err)
	}
	for _, want := range []string{"username=x-access-token\n", "password=" + CredentialPlaceholder + "\n"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("git credential fill = %q, want %q", out, want)
		}
	}
	for _, want := range []string{"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=safe.directory", "GIT_CONFIG_KEY_1=credential.helper", "GH_TOKEN=" + CredentialPlaceholder} {
		if !slices.Contains(environment, want) {
			t.Fatalf("environment %v lacks %s", environment, want)
		}
	}
	own := WithCredentialPlaceholders(dispatch, []string{"GH_TOKEN=own"})
	if slices.Contains(own, "GH_TOKEN="+CredentialPlaceholder) {
		t.Fatalf("an own GH_TOKEN was replaced: %v", own)
	}
	if got := WithCredentialPlaceholders(Dispatch{}, []string{"A=1"}); !slices.Equal(got, []string{"A=1"}) {
		t.Fatalf("a turn without a working directory got %v", got)
	}
}

func TestAsThePerson(t *testing.T) {
	person := func(values ...string) context.Context {
		ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{adk.UserHeader: values}))
		return ctx
	}
	configured := []string{"PATH=/bin", "GIT_AUTHOR_NAME=sandbox", "GIT_COMMITTER_EMAIL=sandbox@localhost", "GIT_CONFIG_COUNT=1"}
	for _, test := range []struct {
		name string
		ctx  context.Context
		want []string
	}{
		{
			name: "the person of the turn is author and committer",
			ctx:  person(" person@example.com "),
			want: []string{
				"PATH=/bin", "GIT_CONFIG_COUNT=1",
				"GIT_AUTHOR_NAME=person@example.com", "GIT_AUTHOR_EMAIL=person@example.com",
				"GIT_COMMITTER_NAME=person@example.com", "GIT_COMMITTER_EMAIL=person@example.com",
			},
		},
		{name: "a turn without a person gets no identity", ctx: person(""), want: []string{"PATH=/bin", "GIT_CONFIG_COUNT=1"}},
		{name: "a call without the header gets no identity", ctx: person(), want: []string{"PATH=/bin", "GIT_CONFIG_COUNT=1"}},
		{name: "no A2A call gets no identity", ctx: t.Context(), want: []string{"PATH=/bin", "GIT_CONFIG_COUNT=1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := AsThePerson(test.ctx, configured); !slices.Equal(got, test.want) {
				t.Fatalf("AsThePerson() = %q, want %q", got, test.want)
			}
			if want := []string{"PATH=/bin", "GIT_AUTHOR_NAME=sandbox", "GIT_COMMITTER_EMAIL=sandbox@localhost", "GIT_CONFIG_COUNT=1"}; !slices.Equal(configured, want) {
				t.Fatalf("input environment mutated to %q", configured)
			}
		})
	}
}

// Package workspace sets up a session's working directory on a workspace
// volume before its first turn: a clone with alternates of every bare mirror
// the volume shares, and a fetch of exactly the repositories that moved since
// the mirrors' last sync. Every Harness runtime that works in such a directory
// uses it, so a session looks the same whichever runtime runs it.
package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
)

// StateFile, relative to the working directory, records that the directory
// was set up. A directory without it is set up again, which a fresh directory
// of a resumed session needs.
const StateFile = ".kagent/workspace.json"

const (
	// DefaultFetchTimeout bounds the fetch of one repository.
	DefaultFetchTimeout = 2 * time.Minute
	// DefaultFetchBudget bounds the fetches of one setup together.
	DefaultFetchBudget = 5 * time.Minute
	// maxReasonBytes bounds the part of git's output a problem reports.
	maxReasonBytes = 512
)

// Dispatch is what a turn's dispatch says about the directory it works in.
type Dispatch struct {
	// Directory is the turn's working directory; empty when the dispatch
	// names none.
	Directory string `json:"directory,omitempty"`
	// Mirrors is the read-only directory of bare mirrors,
	// <owner>/<repository>.git, its clones borrow objects from; empty when the
	// session mounts none.
	Mirrors string `json:"mirrors,omitempty"`
	// Changed lists the repositories, <owner>/<repository>, whose origin moved
	// since the mirrors' last sync.
	Changed []string `json:"changed,omitempty"`
}

// DispatchFrom reads the dispatch of the A2A call that owns ctx. A call
// without one yields the zero Dispatch.
func DispatchFrom(ctx context.Context) Dispatch {
	callCtx, ok := a2asrv.CallContextFrom(ctx)
	if !ok || callCtx == nil || callCtx.ServiceParams() == nil {
		return Dispatch{}
	}
	params := callCtx.ServiceParams()
	first := func(name string) string {
		values, ok := params.Get(name)
		if !ok || len(values) == 0 {
			return ""
		}
		return strings.TrimSpace(values[0])
	}
	dispatch := Dispatch{Directory: first(adk.WorkingDirectoryHeader), Mirrors: first(adk.MirrorsDirectoryHeader)}
	values, _ := params.Get(adk.ChangedRepositoriesHeader)
	// A proxy may join repeated values with commas; both forms parse alike.
	for _, value := range values {
		for repository := range strings.SplitSeq(value, ",") {
			// Only <owner>/<repository> names a clone under the directory.
			if repository = strings.TrimSpace(repository); validRepository(repository) && !slices.Contains(dispatch.Changed, repository) {
				dispatch.Changed = append(dispatch.Changed, repository)
			}
		}
	}
	return dispatch
}

// Options configure how a directory is set up.
type Options struct {
	// Notes is the file name, relative to the working directory, of the notes
	// that tell the agent how the directory is laid out, such as the file the
	// runtime's agent reads on start. Empty writes none.
	Notes string
	// Environment is the environment of every git command, and of the setup
	// PrepareAs runs: the sandbox's own, which reaches the origin through its
	// egress.
	Environment []string
	// FetchTimeout bounds one repository's fetch, FetchBudget all of them;
	// zero takes the defaults.
	FetchTimeout, FetchBudget time.Duration
}

// Identity is the POSIX user and group a setup runs as.
type Identity struct {
	UID, GID uint32
}

// Problem is a repository the setup could not clone or fetch: the turn still
// runs, and the agent works with what the mirror holds.
type Problem struct {
	Repository string `json:"repository"`
	Reason     string `json:"reason"`
}

// Report is what one setup did.
type Report struct {
	// SetUp is true when this call set the directory up; false when it was
	// already, or the dispatch names no workspace.
	SetUp bool `json:"setUp"`
	// Cloned lists the repositories cloned from their mirrors.
	Cloned []string `json:"cloned,omitempty"`
	// Fetched lists the changed repositories fetched from their origin.
	Fetched []string `json:"fetched,omitempty"`
	// Problems lists what could not be cloned or fetched.
	Problems []Problem `json:"problems,omitempty"`
}

// FreshDirectoryNotice tells a returning session that its directory was set
// up again from the workspace's mirrors, such as after the volume's owner
// cleaned up a directory nobody returned to.
const FreshDirectoryNotice = "Workspace setup: this session's directory was set up again from the workspace's mirrors. " +
	"What was pushed is on the provider; what was not pushed is gone."

// TurnNotice is the status message a turn starts with after its setup, or
// empty: a setup that failed with err, the directory a returning session finds
// set up again, and what could not be cloned or fetched.
func (r Report) TurnNotice(err error, returning bool) string {
	var notice []string
	if err != nil {
		notice = append(notice, "Workspace setup failed: "+err.Error())
	}
	if r.SetUp && returning {
		notice = append(notice, FreshDirectoryNotice)
	}
	if problems := r.Notice(); problems != "" {
		notice = append(notice, problems)
	}
	return strings.Join(notice, "\n")
}

// Notice is the one status line a turn reports for the report's problems, or
// empty without any.
func (r Report) Notice() string {
	if len(r.Problems) == 0 {
		return ""
	}
	lines := make([]string, 0, len(r.Problems)+1)
	lines = append(lines, "Workspace setup: these repositories stay at the workspace's last sync.")
	for _, problem := range r.Problems {
		lines = append(lines, fmt.Sprintf("- %s: %s", problem.Repository, problem.Reason))
	}
	return strings.Join(lines, "\n")
}

// state is what StateFile records.
type state struct {
	SetUp    time.Time `json:"setUp"`
	Cloned   []string  `json:"cloned"`
	Fetched  []string  `json:"fetched,omitempty"`
	Problems []Problem `json:"problems,omitempty"`
}

// Prepare sets up the dispatch's directory once: a directory that holds
// StateFile, or a dispatch without mirrors, is left as it is. Every mirror
// gets a clone with alternates at <owner>/<repository> under the directory,
// without a working tree, its origin the mirror's own origin; the changed
// repositories are then fetched from their origin. A repository that fails
// is a Problem, never an error: the error is for a directory that cannot be
// set up at all. The setup runs as the process's own user, who owns the
// directory and everything written into it: a harness that runs as root
// hands the directory to the agent's user and runs the setup as that user
// with PrepareAs.
func Prepare(ctx context.Context, dispatch Dispatch, options Options) (Report, error) {
	if dispatch.Directory == "" || dispatch.Mirrors == "" {
		return Report{}, nil
	}
	if !filepath.IsAbs(dispatch.Directory) || !filepath.IsAbs(dispatch.Mirrors) {
		return Report{}, fmt.Errorf("the working and mirrors directories must be absolute paths")
	}
	// The agent may have left links in its directory on an earlier turn:
	// every file operation stays inside it.
	root, err := os.OpenRoot(dispatch.Directory)
	if err != nil {
		return Report{}, fmt.Errorf("open the working directory: %w", err)
	}
	defer root.Close()
	if _, err := root.Lstat(StateFile); err == nil {
		return Report{}, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Report{}, fmt.Errorf("read the workspace state: %w", err)
	}
	repositories, err := Mirrors(dispatch.Mirrors)
	if err != nil {
		return Report{}, err
	}
	git := gitRunner{root: root, environment: WithCredentialPlaceholders(dispatch, options.Environment)}
	report := Report{SetUp: true}
	for _, repository := range repositories {
		if err := git.clone(ctx, dispatch, repository); err != nil {
			report.Problems = append(report.Problems, Problem{Repository: repository, Reason: "not cloned: " + err.Error()})
			continue
		}
		report.Cloned = append(report.Cloned, repository)
	}
	report.Fetched, report.Problems = git.fetchChanged(ctx, dispatch, report.Cloned, options, report.Problems)
	if options.Notes != "" {
		if err := root.WriteFile(options.Notes, []byte(notes(report.Cloned)), 0o644); err != nil {
			return report, fmt.Errorf("write the workspace notes: %w", err)
		}
	}
	recorded, err := json.Marshal(state{SetUp: time.Now().UTC(), Cloned: report.Cloned, Fetched: report.Fetched, Problems: report.Problems})
	if err != nil {
		return report, err
	}
	if err := root.MkdirAll(path.Dir(StateFile), 0o755); err != nil {
		return report, fmt.Errorf("record the workspace state: %w", err)
	}
	if err := root.WriteFile(StateFile, recorded, 0o644); err != nil {
		return report, fmt.Errorf("record the workspace state: %w", err)
	}
	return report, nil
}

// TrustMirrors returns environment with git told, through GIT_CONFIG_COUNT,
// to trust every repository under the dispatch's mirrors directory: the
// mirrors belong to the volume's owner, and the agent's own git refuses
// another user's repository. Nothing else is trusted. A dispatch without an
// absolute mirrors directory leaves the environment as it is.
func TrustMirrors(dispatch Dispatch, environment []string) []string {
	if !filepath.IsAbs(dispatch.Mirrors) {
		return environment
	}
	// A trailing /* trusts every repository below the directory.
	return withGitConfig(environment, "safe.directory", filepath.Join(dispatch.Mirrors, "*"))
}

// CredentialPlaceholder is the inert credential a workspace turn's git and
// gh send: the egress gateway replaces the Authorization header of a request
// to a provider's host with the person's own credential, and sets none on a
// request that carries no such header. It is never a real token.
const CredentialPlaceholder = "kagent-credential-injected"

// CredentialHelper answers git's request for a credential with the
// placeholder. git asks only after a host challenges a request, so a host
// that answers anonymously never sees it; on a provider's host the gateway
// replaces the Basic header git then sends.
const CredentialHelper = "!f() { test \"$1\" = get && printf 'username=x-access-token\\npassword=" + CredentialPlaceholder + "\\n'; }; f"

// WithCredentialPlaceholders returns environment with the placeholder
// credentials of a turn in the dispatch's working directory: git's credential
// helper (CredentialHelper) and GH_TOKEN, which gh needs before it makes any
// call, unless the environment sets one. A dispatch without an absolute
// working directory leaves the environment as it is.
func WithCredentialPlaceholders(dispatch Dispatch, environment []string) []string {
	if !filepath.IsAbs(dispatch.Directory) {
		return environment
	}
	result := withGitConfig(environment, "credential.helper", CredentialHelper)
	if !slices.ContainsFunc(result, func(variable string) bool { return strings.HasPrefix(variable, "GH_TOKEN=") }) {
		result = append(result, "GH_TOKEN="+CredentialPlaceholder)
	}
	return result
}

// withGitConfig returns environment with key = value added to the git
// configuration GIT_CONFIG_COUNT carries, after the entries it already has.
func withGitConfig(environment []string, key, value string) []string {
	count := 0
	result := make([]string, 0, len(environment)+3)
	for _, variable := range environment {
		if current, ok := strings.CutPrefix(variable, "GIT_CONFIG_COUNT="); ok {
			count, _ = strconv.Atoi(current)
			continue
		}
		result = append(result, variable)
	}
	return append(result,
		"GIT_CONFIG_COUNT="+strconv.Itoa(count+1),
		fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", count, key),
		fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", count, value))
}

// PrepareSubcommand is the argument a harness binary takes to run
// PrepareRequested. A harness that runs as root sets a session directory up
// this way, as the agent's user: the directory is that user's, and inside
// the sandbox root may not write another user's directory on a volume mount.
const PrepareSubcommand = "prepare-workspace"

// Request is what PrepareAs sends its subcommand: the dispatch and the
// options of one Prepare, without the environment, which is the process's
// own.
type Request struct {
	Dispatch     Dispatch      `json:"dispatch"`
	Notes        string        `json:"notes,omitempty"`
	FetchTimeout time.Duration `json:"fetchTimeout,omitempty"`
	FetchBudget  time.Duration `json:"fetchBudget,omitempty"`
}

// Response is what the subcommand answers: the report, and the error of a
// directory that could not be set up at all.
type Response struct {
	Report Report `json:"report"`
	Error  string `json:"error,omitempty"`
}

// PrepareAs runs command, a harness binary with PrepareSubcommand, as runAs
// (the process's own user when nil), in options.Environment, with the
// request on its standard input, and returns what it answered: Prepare as
// the agent's user.
func PrepareAs(ctx context.Context, dispatch Dispatch, options Options, runAs *Identity, command ...string) (Report, error) {
	if len(command) == 0 {
		return Report{}, fmt.Errorf("a command to prepare the workspace with is required")
	}
	request, err := json.Marshal(Request{Dispatch: dispatch, Notes: options.Notes, FetchTimeout: options.FetchTimeout, FetchBudget: options.FetchBudget})
	if err != nil {
		return Report{}, err
	}
	process := exec.CommandContext(ctx, command[0], command[1:]...)
	process.Env = append([]string(nil), options.Environment...)
	process.Stdin = bytes.NewReader(request)
	// The setup's git commands are its group: a cancellation ends them all.
	// The group comes first: it replaces the process attributes RunAs adds to.
	utils.ConfigureProcessGroup(process)
	if runAs != nil {
		utils.RunAs(process, runAs.UID, runAs.GID)
	}
	process.Cancel = func() error { return utils.KillProcessGroup(process.Process) }
	process.WaitDelay = 5 * time.Second
	stderr := utils.NewBoundedBuffer(16 << 10)
	process.Stderr = stderr
	output, err := process.Output()
	if err != nil {
		return Report{}, fmt.Errorf("prepare the workspace as the agent's user: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var response Response
	if err := json.Unmarshal(output, &response); err != nil {
		return Report{}, fmt.Errorf("read the workspace setup's response: %w", err)
	}
	if response.Error != "" {
		return response.Report, errors.New(response.Error)
	}
	return response.Report, nil
}

// PrepareRequested is the subcommand's side of PrepareAs: it reads one
// Request from in, runs Prepare in the process's own environment, and writes
// the Response to out. The setup's own error is the response's; the returned
// error is a request that cannot be read or a response that cannot be
// written.
func PrepareRequested(ctx context.Context, in io.Reader, out io.Writer) error {
	var request Request
	if err := json.NewDecoder(in).Decode(&request); err != nil {
		return fmt.Errorf("read the workspace setup request: %w", err)
	}
	report, err := Prepare(ctx, request.Dispatch, Options{
		Notes: request.Notes, Environment: os.Environ(), FetchTimeout: request.FetchTimeout, FetchBudget: request.FetchBudget,
	})
	response := Response{Report: report}
	if err != nil {
		response.Error = err.Error()
	}
	if err := json.NewEncoder(out).Encode(response); err != nil {
		return fmt.Errorf("write the workspace setup response: %w", err)
	}
	return nil
}

// Mirrors lists the repositories, <owner>/<repository>, whose bare mirror
// <owner>/<repository>.git is under dir, in name order.
func Mirrors(dir string) ([]string, error) {
	owners, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list the mirrors: %w", err)
	}
	var repositories []string
	for _, owner := range owners {
		if !owner.IsDir() || strings.HasPrefix(owner.Name(), ".") {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(dir, owner.Name()))
		if err != nil {
			return nil, fmt.Errorf("list the mirrors of %s: %w", owner.Name(), err)
		}
		for _, entry := range entries {
			name, isMirror := strings.CutSuffix(entry.Name(), ".git")
			if entry.IsDir() && isMirror && name != "" {
				repositories = append(repositories, owner.Name()+"/"+name)
			}
		}
	}
	return repositories, nil
}

// notes tells the agent how the directory is laid out and how to start work
// in a repository.
func notes(cloned []string) string {
	var b strings.Builder
	b.WriteString("# Workspace\n\n")
	b.WriteString("Each repository of this workspace is a git clone at `<owner>/<repository>` in this directory. ")
	b.WriteString("A clone borrows its objects from the workspace's read-only mirror and has no working tree yet: ")
	b.WriteString("check out the default branch where you work, with `git switch <default branch>` in the clone ")
	b.WriteString("(`git -C <owner>/<repository> remote show origin` names it). ")
	b.WriteString("`origin` is the repository on its provider: fetch, branch, commit and push there as usual.\n\n")
	if len(cloned) == 0 {
		b.WriteString("This workspace has no repositories.\n")
		return b.String()
	}
	b.WriteString("Repositories:\n\n")
	for _, repository := range cloned {
		b.WriteString("- `" + repository + "`\n")
	}
	return b.String()
}

type gitRunner struct {
	// root is the working directory, which every file operation stays in.
	root        *os.Root
	environment []string
}

// clone makes the clone with alternates of repository's mirror, with no
// working tree and no local branch: the agent's first `git switch` of the
// default branch creates it from origin, after any fetch.
func (g gitRunner) clone(ctx context.Context, dispatch Dispatch, repository string) error {
	mirror := filepath.Join(dispatch.Mirrors, filepath.FromSlash(repository)+".git")
	clone := filepath.Join(dispatch.Directory, filepath.FromSlash(repository))
	if _, err := g.root.Lstat(repository + "/.git"); err == nil {
		// A setup that stopped before recording its state cloned it already.
		return nil
	}
	if err := g.root.MkdirAll(path.Dir(repository), 0o755); err != nil {
		return err
	}
	// The mirror belongs to the volume's owner, not the agent's user: trust
	// it for this clone only. The clone reads its objects through alternates,
	// which never checks ownership.
	if _, err := g.run(ctx, "", "-c", "safe.directory="+mirror, "clone", "--quiet", "--shared", "--no-checkout",
		// A volume shared over the network: parallel checkout, the untracked
		// cache and the index settings for many files.
		"--config", "checkout.workers=0", "--config", "core.untrackedCache=true", "--config", "feature.manyFiles=true",
		mirror, clone); err != nil {
		return err
	}
	head, err := g.run(ctx, clone, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return err
	}
	if _, err := g.run(ctx, clone, "update-ref", "-d", head); err != nil {
		return err
	}
	origin, err := g.run(ctx, "", "-c", "safe.directory="+mirror, "-C", mirror, "config", "--get", "remote.origin.url")
	if err != nil || origin == "" {
		// A mirror without an origin stays the clone's origin.
		return nil
	}
	_, err = g.run(ctx, clone, "remote", "set-url", "origin", withoutUserinfo(origin))
	return err
}

// fetchChanged fetches every changed repository that was cloned from its
// origin, each under its own timeout and all of them under one budget.
func (g gitRunner) fetchChanged(ctx context.Context, dispatch Dispatch, cloned []string, options Options, problems []Problem) ([]string, []Problem) {
	timeout, budget := options.FetchTimeout, options.FetchBudget
	if timeout <= 0 {
		timeout = DefaultFetchTimeout
	}
	if budget <= 0 {
		budget = DefaultFetchBudget
	}
	budgetCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var fetched []string
	for _, repository := range dispatch.Changed {
		if !slices.Contains(cloned, repository) {
			if !slices.ContainsFunc(problems, func(p Problem) bool { return p.Repository == repository }) {
				problems = append(problems, Problem{Repository: repository, Reason: "not fetched: the workspace has no mirror of it"})
			}
			continue
		}
		if budgetCtx.Err() != nil {
			problems = append(problems, Problem{Repository: repository, Reason: fmt.Sprintf("not fetched: the setup's fetch budget of %s ran out", budget)})
			continue
		}
		fetchCtx, cancelFetch := context.WithTimeout(budgetCtx, timeout)
		_, err := g.run(fetchCtx, filepath.Join(dispatch.Directory, filepath.FromSlash(repository)), "fetch", "--quiet", "--prune", "origin")
		timedOut := errors.Is(fetchCtx.Err(), context.DeadlineExceeded)
		cancelFetch()
		switch {
		case err == nil:
			fetched = append(fetched, repository)
		case timedOut:
			problems = append(problems, Problem{Repository: repository, Reason: fmt.Sprintf("the fetch from origin timed out after %s", timeout)})
		default:
			problems = append(problems, Problem{Repository: repository, Reason: "the origin refused the fetch: " + err.Error()})
		}
	}
	return fetched, problems
}

// run runs git in dir and returns its trimmed standard output; an error
// carries the end of its standard error.
func (g gitRunner) run(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	// No prompt can be answered: a missing credential fails the command.
	command.Env = append(append([]string(nil), g.environment...), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=")
	// A timeout ends git and every helper it started, such as its remote
	// transport, which would otherwise hold the output open.
	utils.ConfigureProcessGroup(command)
	command.Cancel = func() error { return utils.KillProcessGroup(command.Process) }
	command.WaitDelay = 5 * time.Second
	stderr := utils.NewBoundedBuffer(16 << 10)
	command.Stderr = stderr
	output, err := command.Output()
	if err != nil {
		return "", errors.New(reason(stderr.String(), err))
	}
	return strings.TrimSpace(string(output)), nil
}

// reason is the last line of git's error output, bounded, or the exit error
// without one.
func reason(stderr string, err error) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		return err.Error()
	}
	if len(last) > maxReasonBytes {
		last = last[:maxReasonBytes]
	}
	return last
}

// withoutUserinfo drops a credential a mirror's origin URL may carry, so the
// clone keeps none.
func withoutUserinfo(origin string) string {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User == nil || parsed.Scheme == "" {
		return origin
	}
	parsed.User = nil
	return parsed.String()
}

// validRepository reports whether repository is <owner>/<repository>.
func validRepository(repository string) bool {
	owner, name, ok := strings.Cut(repository, "/")
	return ok && owner != "" && name != "" && !strings.Contains(name, "/") && path.Clean(repository) == repository &&
		owner != ".." && name != ".." && owner != "." && name != "."
}

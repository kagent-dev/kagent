// Package workspace sets up a session's working directory on a workspace
// volume before its first turn: a clone with alternates of every bare mirror
// the volume shares, and a fetch of exactly the repositories that moved since
// the mirrors' last sync. Every Harness runtime that works in such a directory
// uses it, so a session looks the same whichever runtime runs it.
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
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
	// Ignored lists the changed entries that name no <owner>/<repository>,
	// which no clone answers to: the setup reports each rather than drop it.
	Ignored []string `json:"ignored,omitempty"`
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
	dispatch := Dispatch{Directory: first(kagenta2a.WorkingDirectoryHeader), Mirrors: first(kagenta2a.MirrorsDirectoryHeader)}
	values, _ := params.Get(kagenta2a.ChangedRepositoriesHeader)
	// A proxy may join repeated values with commas; both forms parse alike.
	for _, value := range values {
		for repository := range strings.SplitSeq(value, ",") {
			// Only <owner>/<repository> names a clone under the directory.
			switch repository = strings.TrimSpace(repository); {
			case repository == "":
			case !kagenta2a.ValidRepository(repository):
				if !slices.Contains(dispatch.Ignored, repository) {
					dispatch.Ignored = append(dispatch.Ignored, repository)
				}
			case !slices.Contains(dispatch.Changed, repository):
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
	// Environment is the environment of every git command: the sandbox's own,
	// which reaches the origin through its egress.
	Environment []string
	// FetchTimeout bounds one repository's fetch, FetchBudget all of them;
	// zero takes the defaults.
	FetchTimeout, FetchBudget time.Duration
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
	// Retried lists the repositories an earlier setup could not clone or
	// fetch that this call tried again; those not among Problems succeeded.
	Retried []string `json:"retried,omitempty"`
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

// Notice is the status a turn reports for the report's retries and problems,
// or empty without any.
func (r Report) Notice() string {
	var lines []string
	recovered := slices.DeleteFunc(slices.Clone(r.Retried), func(repository string) bool {
		return slices.ContainsFunc(r.Problems, func(p Problem) bool { return p.Repository == repository })
	})
	if len(recovered) != 0 {
		lines = append(lines, "Workspace setup: an earlier turn could not clone or fetch these repositories; they are up to date now: "+strings.Join(recovered, ", ")+".")
	}
	if len(r.Problems) == 0 {
		return strings.Join(lines, "\n")
	}
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

// Prepare sets up the dispatch's directory; a dispatch without mirrors is left
// as it is. Every mirror gets a clone with alternates at <owner>/<repository>
// under the directory, without a working tree, its origin the mirror's own
// origin; the changed repositories are then fetched from their origin. A
// repository that fails is a Problem, never an error: the error is for a
// directory that cannot be set up at all. StateFile records the setup: a
// directory whose state records problems has what failed retried on every
// later call, each retry under the same timeout and budget, and a directory
// whose state records none is left as it is. A problem whose repository has
// no mirror is never retried, since nothing could clone it. The setup runs as
// the process's own user, who owns everything written into the directory.
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
	recorded, found, err := readState(root)
	if err != nil {
		return Report{}, err
	}
	git := gitRunner{root: root, environment: options.Environment}
	if found {
		return git.retry(ctx, dispatch, options, recorded)
	}
	repositories, err := Mirrors(dispatch.Mirrors)
	if err != nil {
		return Report{}, err
	}
	report := Report{SetUp: true}
	for _, ignored := range dispatch.Ignored {
		report.Problems = append(report.Problems, Problem{Repository: ignored, Reason: "not fetched: it names no <owner>/<repository>"})
	}
	origins := map[string]string{}
	report.Cloned, report.Problems = git.cloneAll(ctx, dispatch, repositories, origins, report.Problems)
	report.Fetched, report.Problems = git.fetchChanged(ctx, dispatch, dispatch.Changed, origins, options, report.Problems)
	return report, git.record(state{SetUp: time.Now().UTC(), Cloned: report.Cloned, Fetched: report.Fetched, Problems: report.Problems}, options)
}

// cloneAll clones each of repositories, recording each clone's origin in
// origins, and returns the clones and problems with what failed added. A clone
// whose origin cannot be read stands, without an origin, and is a problem too.
func (g gitRunner) cloneAll(ctx context.Context, dispatch Dispatch, repositories []string, origins map[string]string, problems []Problem) ([]string, []Problem) {
	var cloned []string
	for _, repository := range repositories {
		origin, err := g.clone(ctx, dispatch, repository)
		var unknown originUnknownError
		switch {
		case errors.As(err, &unknown):
			// The clone stands, without an origin: the agent works with what
			// the mirror holds.
			problems = append(problems, Problem{Repository: repository, Reason: err.Error()})
		case err != nil:
			problems = append(problems, Problem{Repository: repository, Reason: "not cloned: " + err.Error()})
			continue
		}
		cloned = append(cloned, repository)
		origins[repository] = origin
	}
	return cloned, problems
}

// retry tries again what the recorded setup could not clone or fetch: every
// such repository is cloned again, which for a clone in place sets its origin
// again, and fetched when the dispatch names it changed. The state then
// records the outcome, and what failed again stays a problem for the next
// call.
func (g gitRunner) retry(ctx context.Context, dispatch Dispatch, options Options, recorded state) (Report, error) {
	var retried []string
	var unretryable []Problem
	for _, problem := range recorded.Problems {
		switch {
		case !hasMirror(dispatch.Mirrors, problem.Repository):
			unretryable = append(unretryable, problem)
		case !slices.Contains(retried, problem.Repository):
			retried = append(retried, problem.Repository)
		}
	}
	if len(retried) == 0 {
		return Report{}, nil
	}
	report := Report{Retried: retried}
	origins := map[string]string{}
	var cloned []string
	cloned, report.Problems = g.cloneAll(ctx, dispatch, retried, origins, nil)
	for _, repository := range cloned {
		if !slices.Contains(recorded.Cloned, repository) {
			report.Cloned = append(report.Cloned, repository)
		}
	}
	changed := slices.DeleteFunc(slices.Clone(dispatch.Changed), func(repository string) bool { return !slices.Contains(retried, repository) })
	report.Fetched, report.Problems = g.fetchChanged(ctx, dispatch, changed, origins, options, report.Problems)
	updated := state{
		SetUp: recorded.SetUp, Cloned: union(recorded.Cloned, report.Cloned), Fetched: union(recorded.Fetched, report.Fetched),
		Problems: append(unretryable, report.Problems...),
	}
	if len(report.Cloned) == 0 {
		// No new clone for the notes to name.
		return report, g.writeState(updated)
	}
	return report, g.record(updated, options)
}

// union is a with every element of b it lacks appended, in order.
func union(a, b []string) []string {
	result := slices.Clone(a)
	for _, element := range b {
		if !slices.Contains(result, element) {
			result = append(result, element)
		}
	}
	return result
}

// record writes what a setup leaves beside its clones: the notes and the
// state.
func (g gitRunner) record(recorded state, options Options) error {
	if options.Notes != "" {
		if err := g.root.WriteFile(options.Notes, []byte(notes(recorded.Cloned)), 0o644); err != nil {
			return fmt.Errorf("write the workspace notes: %w", err)
		}
	}
	return g.writeState(recorded)
}

// readState reads StateFile; found is false for a directory without one. A
// state that does not parse, which the agent may have overwritten, records a
// setup with nothing to retry.
func readState(root *os.Root) (recorded state, found bool, err error) {
	data, err := root.ReadFile(StateFile)
	if errors.Is(err, fs.ErrNotExist) {
		return state{}, false, nil
	}
	if err != nil {
		return state{}, false, fmt.Errorf("read the workspace state: %w", err)
	}
	if json.Unmarshal(data, &recorded) != nil {
		return state{}, true, nil
	}
	return recorded, true, nil
}

// writeState records the setup in StateFile.
func (g gitRunner) writeState(recorded state) error {
	data, err := json.Marshal(recorded)
	if err != nil {
		return err
	}
	if err := g.root.MkdirAll(path.Dir(StateFile), 0o755); err != nil {
		return fmt.Errorf("record the workspace state: %w", err)
	}
	if err := g.root.WriteFile(StateFile, data, 0o644); err != nil {
		return fmt.Errorf("record the workspace state: %w", err)
	}
	return nil
}

// hasMirror reports whether repository, <owner>/<repository>, has a bare
// mirror under mirrors.
func hasMirror(mirrors, repository string) bool {
	if !kagenta2a.ValidRepository(repository) {
		return false
	}
	info, err := os.Stat(filepath.Join(mirrors, filepath.FromSlash(repository)+".git"))
	return err == nil && info.IsDir()
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
	count := 0
	result := make([]string, 0, len(environment)+3)
	for _, variable := range environment {
		if value, ok := strings.CutPrefix(variable, "GIT_CONFIG_COUNT="); ok {
			count, _ = strconv.Atoi(value)
			continue
		}
		result = append(result, variable)
	}
	// A trailing /* trusts every repository below the directory.
	return append(result,
		"GIT_CONFIG_COUNT="+strconv.Itoa(count+1),
		fmt.Sprintf("GIT_CONFIG_KEY_%d=safe.directory", count),
		fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", count, filepath.Join(dispatch.Mirrors, "*")))
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

// originReadAttempts and originReadBackoff bound the reads of a mirror's
// origin: the workspace's sync rewrites the mirror's configuration by lock and
// rename while a setup may read it, so a failed read is tried again.
var (
	originReadAttempts = 3
	originReadBackoff  = 250 * time.Millisecond
)

// originUnknownError is a clone whose mirror's origin could not be read: the
// clone stands, without an origin.
type originUnknownError struct {
	cause error
}

func (e originUnknownError) Error() string {
	return "origin unknown: " + e.cause.Error()
}

// clone makes the clone with alternates of repository's mirror, with no
// working tree and no local branch: the agent's first `git switch` of the
// default branch creates it from origin, after any fetch. It returns the
// clone's origin: the mirror's own origin, or the mirror itself when it has
// none. An origin that cannot be read is an originUnknownError, and the clone
// is left without an origin rather than with the mirror's path.
func (g gitRunner) clone(ctx context.Context, dispatch Dispatch, repository string) (string, error) {
	mirror := filepath.Join(dispatch.Mirrors, filepath.FromSlash(repository)+".git")
	clone := filepath.Join(dispatch.Directory, filepath.FromSlash(repository))
	// A setup that stopped before recording its state cloned it already, and
	// only its origin is set again.
	if _, err := g.root.Lstat(repository + "/.git"); err != nil {
		if err := g.root.MkdirAll(path.Dir(repository), 0o755); err != nil {
			return "", err
		}
		// The mirror belongs to the volume's owner, not the agent's user: trust
		// it for this clone only. The clone reads its objects through alternates,
		// which never checks ownership.
		if _, err := g.run(ctx, "", "-c", "safe.directory="+mirror, "clone", "--quiet", "--shared", "--no-checkout",
			// A volume shared over the network: parallel checkout, the untracked
			// cache and the index settings for many files.
			"--config", "checkout.workers=0", "--config", "core.untrackedCache=true", "--config", "feature.manyFiles=true",
			mirror, clone); err != nil {
			return "", err
		}
		head, err := g.run(ctx, clone, "symbolic-ref", "--quiet", "HEAD")
		if err != nil {
			return "", err
		}
		if _, err := g.run(ctx, clone, "update-ref", "-d", head); err != nil {
			return "", err
		}
	}
	origin, err := g.mirrorOrigin(ctx, mirror)
	if err != nil {
		// The clone's origin is the mirror's path until it is set: unset it.
		// The clone has an origin, so only a failure to unset it remains.
		if _, unsetErr := g.run(ctx, clone, "config", "--unset", "remote.origin.url"); unsetErr != nil && !isExitStatus(unsetErr, 5) {
			return "", fmt.Errorf("unset the mirror's path as origin: %w", unsetErr)
		}
		return "", originUnknownError{cause: err}
	}
	if origin == "" {
		// A mirror without an origin stays the clone's origin.
		origin = mirror
	}
	_, err = g.run(ctx, clone, "config", "remote.origin.url", origin)
	return origin, err
}

// mirrorOrigin reads the origin of mirror, without user information: empty
// for a mirror without one (git config's exit status 1). Any other failure is
// tried again, and the last one returned.
func (g gitRunner) mirrorOrigin(ctx context.Context, mirror string) (string, error) {
	var err error
	for attempt := range originReadAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", err
			case <-time.After(originReadBackoff):
			}
		}
		var origin string
		origin, err = g.run(ctx, "", "-c", "safe.directory="+mirror, "-C", mirror, "config", "--get", "remote.origin.url")
		switch {
		case err == nil:
			return withoutUserinfo(origin), nil
		case isExitStatus(err, 1):
			return "", nil
		}
	}
	return "", err
}

// providerOrigin reports whether origin is a repository on its provider, a
// URL or scp-like address, rather than a path on the local filesystem such as
// a mirror's.
func providerOrigin(origin string) bool {
	if parsed, err := url.Parse(origin); err == nil && parsed.Scheme != "" {
		return true
	}
	// host:path, git's scp-like syntax, has its colon before any slash.
	colon := strings.Index(origin, ":")
	return colon > 0 && !strings.Contains(origin[:colon], "/")
}

// fetchChanged fetches every repository of changed that was cloned, origins
// naming each clone's origin, from that origin, each under its own timeout and
// all of them under one budget. A clone without a provider origin is never
// fetched: a fetch from the mirror moves nothing.
func (g gitRunner) fetchChanged(ctx context.Context, dispatch Dispatch, changed []string, origins map[string]string, options Options, problems []Problem) ([]string, []Problem) {
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
	for _, repository := range changed {
		hasProblem := slices.ContainsFunc(problems, func(p Problem) bool { return p.Repository == repository })
		origin, cloned := origins[repository]
		if !cloned {
			if !hasProblem {
				problems = append(problems, Problem{Repository: repository, Reason: "not fetched: the workspace has no mirror of it"})
			}
			continue
		}
		if !providerOrigin(origin) {
			// A clone whose origin is unknown has its problem already.
			if !hasProblem {
				problems = append(problems, Problem{Repository: repository, Reason: "not fetched: no provider origin"})
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
			problems = append(problems, Problem{Repository: repository, Reason: "the fetch from origin failed: " + err.Error()})
		}
	}
	return fetched, problems
}

// gitError is a git command that failed: its reason and exit status.
type gitError struct {
	reason string
	status int
}

func (e *gitError) Error() string { return e.reason }

// isExitStatus reports whether err is a git command that exited with status.
func isExitStatus(err error, status int) bool {
	var failed *gitError
	return errors.As(err, &failed) && failed.status == status
}

// run runs git in dir and returns its trimmed standard output; an error is a
// *gitError carrying the cause from its standard error.
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
		failed := &gitError{reason: reason(stderr.String(), err), status: -1}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			failed.status = exitErr.ExitCode()
		}
		return "", failed
	}
	return strings.TrimSpace(string(output)), nil
}

// reason is git's error output reduced to its cause, bounded: the first
// fatal: or error: line, which names the cause, and the last line when it
// differs, which names where git stopped; the last line alone without such a
// line, or the exit error without any output.
func reason(stderr string, err error) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		return err.Error()
	}
	result := last
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "fatal:") || strings.HasPrefix(line, "error:") {
			if line != last {
				result = line + "; " + last
			}
			break
		}
	}
	if len(result) > maxReasonBytes {
		result = result[:maxReasonBytes]
	}
	return result
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

package a2a

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/adk/pkg/turn"
	"github.com/kagent-dev/kagent/go/harness/workspace"
)

const (
	// workspaceNotes is the file the setup explains a session directory in;
	// the execution tools' working-directory rule points the model at it.
	workspaceNotes = "AGENTS.md"
	// freshDirectoryNotice tells a returning session that its directory was
	// set up again from the workspace's mirrors, such as after the volume's
	// owner cleaned up a directory nobody returned to.
	freshDirectoryNotice = "Workspace setup: this session's directory was set up again from the workspace's mirrors. " +
		"What was pushed is on the provider; what was not pushed is gone."
)

// prepareTurnDirectory returns ctx with the directory the turn's tools work
// in, the one its dispatch names, read per turn because a runtime restored
// from a snapshot predates the session's mounts, and the status message the
// turn starts with, or empty. A session directory with mirrors is set up
// before its first turn: a clone with alternates of every mirror, then a fetch
// of exactly the repositories the dispatch names as changed. The fetch runs in
// the dispatched turn, so it reaches the origin through the egress the turn's
// caller is set on; the sandbox holds no credential. What could not be cloned
// or fetched, or a setup that failed, is the status message, and the turn
// runs. A resumed turn continues the turn that set the directory up and sets
// up nothing; a returning session whose directory is set up again is told so.
func (e *KAgentExecutor) prepareTurnDirectory(ctx context.Context, resumed, returning bool) (context.Context, string) {
	dispatch := workspace.DispatchFrom(ctx)
	if !filepath.IsAbs(dispatch.Directory) {
		return ctx, ""
	}
	if err := os.MkdirAll(dispatch.Directory, 0o755); err != nil {
		// The tools keep the scratch directory rather than fail every command.
		e.logger.WarnContext(ctx, "the turn's working directory is unavailable", "directory", dispatch.Directory, "error", err)
		return ctx, ""
	}
	ctx = turn.WithDirectory(ctx, dispatch.Directory)
	if resumed || dispatch.Mirrors == "" {
		return ctx, ""
	}
	report, err := workspace.Prepare(ctx, dispatch, workspace.Options{
		Notes: workspaceNotes,
		// The environment of the turn's own commands: git reaches the origin
		// as bash does.
		Environment: os.Environ(),
	})
	if report.SetUp {
		e.logger.DebugContext(ctx, "session directory set up", "directory", dispatch.Directory,
			"cloned", report.Cloned, "fetched", report.Fetched, "problems", len(report.Problems))
	}
	var notice []string
	if err != nil {
		notice = append(notice, "Workspace setup failed: "+err.Error())
	}
	if report.SetUp && returning {
		notice = append(notice, freshDirectoryNotice)
	}
	if problems := report.Notice(); problems != "" {
		notice = append(notice, problems)
	}
	return ctx, strings.Join(notice, "\n")
}

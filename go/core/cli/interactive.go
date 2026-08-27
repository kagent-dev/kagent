package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/kagent-dev/kagent/go/core/cli/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// runInteractive launches the workspace; the TUI reads raw keys, so a redirected stream is an error.
func runInteractive(cmd *cobra.Command, cfg *connection.Options) (err error) {
	if !isTerminal(cmd.InOrStdin()) || !isTerminal(cmd.OutOrStdout()) {
		return errors.New("kagent requires a terminal; use `kagent get agent-instance` and `kagent invoke` for non-interactive use")
	}

	client := cfg.Client()
	defer func() {
		err = errors.Join(err, client.Close())
	}()

	portForward, connectErr := connection.Connect(cmd.Context(), cfg)
	if connectErr != nil {
		return fmt.Errorf("connect to kagent: %w", connectErr)
	}
	if portForward != nil {
		defer portForward.Stop()
	}

	workspace := tui.Options{Namespace: cfg.Namespace}
	if runErr := tui.RunWorkspace(cmd.Context(), workspace, client, cfg.Verbose); runErr != nil {
		return fmt.Errorf("run kagent workspace: %w", runErr)
	}
	return nil
}

// isTerminal reports whether a stream is backed by a TTY; a non-*os.File never is.
func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

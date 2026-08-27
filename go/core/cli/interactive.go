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
func runInteractive(cmd *cobra.Command, _ []string) (err error) {
	if !isTerminal(cmd.InOrStdin()) || !isTerminal(cmd.OutOrStdout()) {
		return errors.New("kagent requires a terminal; use `kagent get agent-instance` and `kagent invoke` for non-interactive use")
	}

	options, err := connection.OptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	session, err := connection.Open(cmd.Context(), options)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, session.Close())
	}()

	workspace := tui.Options{Namespace: session.Namespace}
	if runErr := tui.RunWorkspace(cmd.Context(), workspace, session.Client, options.Verbose); runErr != nil {
		return fmt.Errorf("run kagent workspace: %w", runErr)
	}
	return nil
}

// isTerminal reports whether a stream is backed by a TTY; a non-*os.File never is.
func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

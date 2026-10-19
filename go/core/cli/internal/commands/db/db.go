// Package db wires the CLI configuration to the database commands.
package db

import (
	"context"

	"github.com/spf13/cobra"

	cliconfig "github.com/kagent-dev/kagent/go/core/cli/internal/config"
	dbcli "github.com/kagent-dev/kagent/go/core/cli/internal/db"
	"github.com/kagent-dev/kagent/go/core/cli/internal/db/migrate"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

// NewDBCmd builds the database commands. extra, when set, adds migration
// tracks after the builtin ones. It runs when a db subcommand runs, after the
// configuration loads, not when the command tree is built.
func NewDBCmd(extra migrate.SourcesFunc) *cobra.Command {
	cmd := dbcli.NewCommandFromFunc(func(ctx context.Context) ([]migrations.Source, error) {
		cfg := cliconfig.FromContext(ctx).GetDatabase()
		sources := migrations.BuiltinSourcesInSchema(cfg.GetVectorEnabled(), cfg.GetSchema(), cfg.GetVectorSchema())
		if extra == nil {
			return sources, nil
		}
		extraSources, err := extra(ctx)
		if err != nil {
			return nil, err
		}
		return append(sources, extraSources...), nil
	})
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if err := cmd.Root().PersistentPreRunE(cmd, args); err != nil {
			return err
		}
		owner := cliconfig.FromContext(cmd.Context()).GetDatabase().GetOwner()
		flags := cmd.Flags()
		if flag := flags.Lookup("db-role"); flag != nil && !flag.Changed {
			if err := flag.Value.Set(owner.GetRole()); err != nil {
				return err
			}
		}
		if flag := flags.Lookup("db-url-file"); flag != nil && !flag.Changed && !flags.Changed("db-url") {
			if err := flag.Value.Set(owner.GetUrlFile()); err != nil {
				return err
			}
		}
		return nil
	}
	return cmd
}

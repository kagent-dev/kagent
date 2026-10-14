// Package db wires the CLI configuration to the database commands.
package db

import (
	"context"

	"github.com/spf13/cobra"

	cliconfig "github.com/kagent-dev/kagent/go/core/cli/internal/config"
	dbcli "github.com/kagent-dev/kagent/go/core/cli/internal/db"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

func NewDBCmd() *cobra.Command {
	cmd := dbcli.NewCommandFromFunc(func(ctx context.Context) ([]migrations.Source, error) {
		cfg := cliconfig.FromContext(ctx).GetDatabase()
		return migrations.BuiltinSourcesInSchema(cfg.GetVectorEnabled(), cfg.GetSchema(), cfg.GetVectorSchema()), nil
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

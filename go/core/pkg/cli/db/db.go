// Package db hosts the `kagent db` parent command and its subcommands.
// Currently only `migrate` is wired; future siblings attach here.
package db

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/kagent-dev/kagent/go/core/pkg/cli/db/migrate"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

// NewCommand returns the `db` parent command with `migrate` attached. The
// given sources define the migration tracks the subcommands operate on, in
// orchestrator registration order.
func NewCommand(sources ...migrations.Source) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database operations (migrations, inspection)",
	}
	cmd.AddCommand(migrate.NewCommand(sources...))

	// Hide the root's API-oriented persistent flags from help across the
	// entire `db` subtree. They target the kagent server / Kubernetes,
	// but db commands talk to Postgres directly via --db-url.
	//
	// Hidden is a property of the flag itself (shared across the whole
	// tree), so we can't flip it permanently. The HelpFunc override
	// toggles it for the duration of the help render and restores it
	// after. Children of `db` that don't set their own HelpFunc walk the
	// parent chain and pick this one up.
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		for _, name := range []string{"config", "kagent-url", "namespace", "output-format", "timeout", "verbose"} {
			if f := c.InheritedFlags().Lookup(name); f != nil {
				f.Hidden = true
				defer func(f *pflag.Flag) { f.Hidden = false }(f)
			}
		}
		c.Root().HelpFunc()(c, args)
	})

	return cmd
}

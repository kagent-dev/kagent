package migrate

import (
	"fmt"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/a2amigration"
	"github.com/spf13/cobra"
)

const (
	defaultPostgresURL = "postgres://postgres:kagent@kagent-postgresql.kagent.svc.cluster.local:5432/postgres"
)

type A2ADataOptions struct {
	PostgresDatabaseURL     string
	PostgresDatabaseURLFile string
	BatchSize               int
	DryRun                  bool
}

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run kagent data migrations",
	}
	cmd.AddCommand(newA2ADataCommand())
	return cmd
}

func newA2ADataCommand() *cobra.Command {
	opts := A2ADataOptions{
		PostgresDatabaseURL: defaultPostgresURL,
		BatchSize:           100,
	}

	cmd := &cobra.Command{
		Use:   "a2a-v1",
		Short: "Migrate stored A2A task and push notification data to v1",
		Long: `Migrate stored A2A task and push notification JSON blobs from the legacy
trpc-a2a-go shape to the official A2A v1 shape. Take a database backup before running without --dry-run.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			url, err := database.ResolveURL(opts.PostgresDatabaseURL, opts.PostgresDatabaseURLFile)
			if err != nil {
				return err
			}
			db, err := database.Connect(cmd.Context(), &database.PostgresConfig{URL: url})
			if err != nil {
				return err
			}
			defer db.Close()

			stats, err := a2amigration.Run(cmd.Context(), db, a2amigration.Options{
				BatchSize: opts.BatchSize,
				DryRun:    opts.DryRun,
				Out:       cmd.OutOrStdout(),
			})
			if err != nil {
				return err
			}

			mode := "migration"
			rowVerb := "migrated"
			if opts.DryRun {
				mode = "dry-run"
				rowVerb = "would migrate"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "A2A data migration complete (%s):\n", mode)
			fmt.Fprintf(cmd.OutOrStdout(), "  tasks: %d %s, %d skipped, %d failed\n", stats.TasksMigrated, rowVerb, stats.TasksSkipped, stats.TasksFailed)
			fmt.Fprintf(cmd.OutOrStdout(), "  push notifications: %d %s, %d skipped, %d failed\n", stats.PushNotificationsMigrated, rowVerb, stats.PushNotificationsSkipped, stats.PushNotificationsFailed)
			fmt.Fprintf(cmd.OutOrStdout(), "  already v1: %d\n", stats.AlreadyV1)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.PostgresDatabaseURL, "postgres-database-url", opts.PostgresDatabaseURL, "The URL of the PostgreSQL database.")
	cmd.Flags().StringVar(&opts.PostgresDatabaseURLFile, "postgres-database-url-file", "", "Path to a file containing the PostgreSQL database URL. Takes precedence over --postgres-database-url.")
	cmd.Flags().IntVar(&opts.BatchSize, "batch-size", opts.BatchSize, "Number of legacy rows to process per batch.")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Convert rows and report counts without writing changes.")
	return cmd
}

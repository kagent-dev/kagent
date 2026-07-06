// Package migrate exposes the `kagent db migrate` subcommand: out-of-band
// application, rollback, and inspection of the database migration tracks.
//
// The command operates on the same migrations.Source values app.Start
// registers and opens migrators through migrations.WithMigrator, so every
// operation shares the orchestrator's schema handling, tracking tables, and
// advisory-lock identity — a CLI invocation racing a booting server
// serializes instead of corrupting state.
//
// Unlike the in-app startup path, the CLI never auto-recovers a dirty
// tracking table: up, down, and goto refuse a dirty source and report the
// `force` invocation that clears it. Deliberate recovery is the operator's
// call.
package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/spf13/cobra"

	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

const (
	// dbURLEnv is the controller's env var for --postgres-database-url
	// (see app.LoadFromEnv); the CLI falls back to it so an operator with
	// the controller's environment needs no extra flags.
	dbURLEnv   = "POSTGRES_DATABASE_URL"
	sourceFlag = "source"
)

// sourceNameRE constrains Source.Name to lowercase identifiers with
// underscores or hyphens (e.g. "core", "vector", and hyphenated names from
// downstream-registered sources). Name flows into `--source <name>` handling
// and operator-facing messages; the regex keeps those strings predictable.
var sourceNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

type commandState struct {
	dbURL   string
	source  string
	sources []migrations.Source
}

// NewCommand returns the `migrate` parent command with all subcommands
// attached, operating on the given sources in orchestrator registration
// order. Panics on an invalid or duplicate source Name so misconfiguration
// fails at wiring time, not mid-operation.
func NewCommand(sources ...migrations.Source) *cobra.Command {
	seen := map[string]bool{}
	for _, source := range sources {
		if !sourceNameRE.MatchString(source.Name) {
			panic(fmt.Sprintf("migrate.NewCommand: source Name=%q must match %s", source.Name, sourceNameRE.String()))
		}
		if seen[source.Name] {
			panic(fmt.Sprintf("migrate.NewCommand: source %q already configured; each source must have a unique Name", source.Name))
		}
		seen[source.Name] = true
	}
	state := &commandState{sources: append([]migrations.Source(nil), sources...)}

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply, roll back, and inspect database migrations",
		Long: `Apply, roll back, and inspect database migrations independently
of server startup. Reads ` + dbURLEnv + ` from the environment when
--db-url is omitted.`,
	}
	cmd.PersistentFlags().StringVar(&state.dbURL, "db-url", "",
		"PostgreSQL connection URL (defaults to value of "+dbURLEnv+" env var)")
	cmd.PersistentFlags().StringVar(&state.source, sourceFlag, "",
		"Migration source name for per-source ops (down/goto/force/version); inferred when only one source is registered. Not applicable to up or status — those aggregate across every registered source.")

	cmd.AddCommand(newUpCmd(state))
	cmd.AddCommand(newDownCmd(state))
	cmd.AddCommand(newStatusCmd(state))
	cmd.AddCommand(newVersionCmd(state))
	cmd.AddCommand(newGotoCmd(state))
	cmd.AddCommand(newForceCmd(state))
	return cmd
}

func (s *commandState) resolveDSN() (string, error) {
	dsn := strings.TrimSpace(s.dbURL)
	if dsn == "" {
		dsn = os.Getenv(dbURLEnv)
	}
	if dsn == "" {
		return "", fmt.Errorf("database URL not set; pass --db-url or set %s", dbURLEnv)
	}
	return dsn, nil
}

// resolveSource picks the source for a per-source operation. With one source
// registered it's returned directly; with more than one the operator must
// pass --source and we report the registered set when they don't.
func (s *commandState) resolveSource() (migrations.Source, error) {
	srcs := s.sources
	if len(srcs) == 0 {
		return migrations.Source{}, errors.New("no migration sources registered")
	}
	if len(srcs) == 1 {
		if s.source != "" && s.source != srcs[0].Name {
			return migrations.Source{}, fmt.Errorf("--source %q not registered; registered source: %s", s.source, srcs[0].Name)
		}
		return srcs[0], nil
	}
	if s.source == "" {
		return migrations.Source{}, fmt.Errorf("registered sources: %s; pass --source", sourceNames(srcs))
	}
	for _, src := range srcs {
		if src.Name == s.source {
			return src, nil
		}
	}
	return migrations.Source{}, fmt.Errorf("--source %q not registered; registered sources: %s", s.source, sourceNames(srcs))
}

func sourceNames(srcs []migrations.Source) string {
	names := make([]string, len(srcs))
	for i, s := range srcs {
		names[i] = s.Name
	}
	return strings.Join(names, ", ")
}

// readVersion returns mg's highest applied version and whether the tracking
// row is dirty (mid-failed-migration). ErrNilVersion (nothing applied) is
// normalized to (0, false, nil).
func readVersion(mg *migrate.Migrate) (uint, bool, error) {
	v, dirty, err := mg.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("read version: %w", err)
	}
	return v, dirty, nil
}

// ensureClean returns an actionable error when src's tracking table is dirty.
// The CLI refuses to operate on a dirty source rather than auto-recovering
// (the in-app startup path is the automatic tier; the CLI is the manual one).
func ensureClean(src migrations.Source, mg *migrate.Migrate) error {
	v, dirty, err := readVersion(mg)
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("source %q is dirty at version %d (a previous migration attempt failed and must be resolved manually); inspect the schema, then clear the flag with \"kagent db migrate force <version> --source %s\" where <version> is the version the schema actually reflects",
			src.Name, v, src.Name)
	}
	return nil
}

// sourceFileVersions returns the (ascending-sorted) NNN versions parsed from
// every NNN_name.up.sql file in src.FS/src.Dir.
//
// The set isn't required to be contiguous — gaps (e.g. a deleted 005) are
// real and the missing numbers are treated as not-applicable-to-this-binary.
// status/desync math goes via this list so the count of files and the highest
// version stay distinct.
func sourceFileVersions(src migrations.Source) ([]int, error) {
	entries, err := fs.ReadDir(src.FS, src.Dir)
	if err != nil {
		return nil, fmt.Errorf("read migration dir %s: %w", src.Dir, err)
	}
	var versions []int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 {
			continue
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		versions = append(versions, v)
	}
	slices.Sort(versions)
	return versions, nil
}

// lineRow carries per-source status data through the status command's text
// and JSON output paths.
type lineRow struct {
	src        migrations.Source
	applied    int
	pending    int
	dbVersion  int  // raw DB version for desync reporting
	downgraded bool // dbVersion > highest shipped version
	dirty      bool // mid-failed-migration; surfaced as a (dirty) annotation
}

func newUpCmd(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations across every registered source",
		Long: `Applies pending migrations for every registered source in
registration order, through the same orchestrator the server runs at
startup: per-source advisory locking, pre-run version snapshots, and
compensating rollback of earlier sources when a later one fails.

Refuses to run while any source's tracking table is dirty; clear it
with 'force' first.

The --source flag is intentionally not applicable to up; pass it only
on the per-source subcommands (down/goto/force).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if state.source != "" {
				return errors.New("up aggregates across all registered sources; --source is not applicable")
			}
			dsn, err := state.resolveDSN()
			if err != nil {
				return err
			}
			srcs := state.sources
			if len(srcs) == 0 {
				return errors.New("no migration sources registered")
			}

			ctx := cmd.Context()
			// One pre-pass over the sources: refuse dirty state, and snapshot
			// pending counts so we can report "applied N migration(s)" after
			// the orchestrator succeeds.
			prePending := 0
			for _, src := range srcs {
				p, err := pendingCount(ctx, src, dsn)
				if err != nil {
					return err
				}
				prePending += p
			}

			if err := migrations.RunUp(ctx, dsn, srcs); err != nil {
				return err
			}

			if prePending == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no pending migrations; schema is up to date")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "applied %d migration(s); schema is up to date\n", prePending)
			return nil
		},
	}
}

// pendingCount counts NNN_*.up.sql files whose version is greater than the
// source's current applied version, refusing a dirty source. Uses the same
// `sourceFileVersions` primitive as `status` so the two paths can't drift.
func pendingCount(ctx context.Context, src migrations.Source, dsn string) (int, error) {
	versions, err := sourceFileVersions(src)
	if err != nil {
		return 0, err
	}
	var pending int
	err = migrations.WithMigrator(ctx, dsn, src, func(mg *migrate.Migrate) error {
		if err := ensureClean(src, mg); err != nil {
			return err
		}
		v, _, verr := readVersion(mg)
		if verr != nil {
			return verr
		}
		for _, fv := range versions {
			if uint(fv) > v {
				pending++
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return pending, nil
}

func newDownCmd(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use:   "down N",
		Short: "Roll back the N most-recent applied migrations for the selected source",
		Long: `Roll back the N most-recent applied migrations for the selected source.

Down migrations can lose data by design — a rolled-back column loses
its contents. Refuses to run while the source's tracking table is
dirty; clear it with 'force' first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[0])
			if err != nil || n < 1 {
				return fmt.Errorf("expected a positive integer for N, got %q", args[0])
			}
			dsn, err := state.resolveDSN()
			if err != nil {
				return err
			}
			src, err := state.resolveSource()
			if err != nil {
				return err
			}
			return migrations.WithMigrator(cmd.Context(), dsn, src, func(mg *migrate.Migrate) error {
				if err := ensureClean(src, mg); err != nil {
					return err
				}
				preV, _, err := readVersion(mg)
				if err != nil {
					return err
				}
				// Guarded here rather than relying on golang-migrate: Steps
				// on an empty track reports a confusing "file does not
				// exist" instead of ErrNoChange.
				if preV == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no migrations to roll back")
					return nil
				}
				if err := mg.Steps(-n); err != nil {
					if errors.Is(err, migrate.ErrNoChange) {
						fmt.Fprintln(cmd.OutOrStdout(), "no migrations to roll back")
						return nil
					}
					return err
				}
				postV, _, verr := readVersion(mg)
				if verr != nil {
					return fmt.Errorf("read version after rollback: %w", verr)
				}
				rolled := countVersionsBetween(src, postV, preV)
				fmt.Fprintf(cmd.OutOrStdout(), "rolled back %d migration(s)\n", rolled)
				return nil
			})
		},
	}
}

func newStatusCmd(state *commandState) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show how many migrations are applied vs pending across all sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if state.source != "" {
				return errors.New("status aggregates across all registered sources; --source is not applicable")
			}
			if output != "text" && output != "json" {
				return fmt.Errorf("invalid --output %q; supported: text, json", output)
			}
			dsn, err := state.resolveDSN()
			if err != nil {
				return err
			}
			srcs := state.sources
			if len(srcs) == 0 {
				return errors.New("no migration sources registered")
			}

			lines := make([]lineRow, 0, len(srcs))
			appliedTotal, pendingTotal := 0, 0
			// Single-source builds print no per-source breakdown, so the
			// stderr desync warning below is gated to them as their only
			// signal; multi-source builds carry it in the stdout breakdown.
			multiSource := len(srcs) > 1
			for _, src := range srcs {
				versions, err := sourceFileVersions(src)
				if err != nil {
					return err
				}
				maxFileVersion := 0
				if len(versions) > 0 {
					maxFileVersion = versions[len(versions)-1]
				}
				var applied, dbVersion int
				var downgraded, dirty bool
				if rerr := migrations.WithMigrator(cmd.Context(), dsn, src, func(mg *migrate.Migrate) error {
					v, d, err := readVersion(mg)
					if err != nil {
						return err
					}
					dbVersion = int(v)
					dirty = d
					// Count files at/below the DB version as applied
					// (counting by version, not file count, survives gaps
					// like a deleted v5).
					for _, fv := range versions {
						if fv <= dbVersion {
							applied++
						}
					}
					if dbVersion > maxFileVersion {
						// Older binary against a DB migrated by a newer
						// build. Warn, don't fail.
						downgraded = true
						if !multiSource {
							fmt.Fprintf(cmd.ErrOrStderr(),
								"warning: %s reports version %d but this binary's highest shipped migration is v%d (older binary against newer DB?)\n",
								src.Name, v, maxFileVersion)
						}
					}
					return nil
				}); rerr != nil {
					return rerr
				}
				pending := len(versions) - applied
				lines = append(lines, lineRow{src: src, applied: applied, pending: pending, dbVersion: dbVersion, downgraded: downgraded, dirty: dirty})
				appliedTotal += applied
				pendingTotal += pending
			}

			out := cmd.OutOrStdout()
			if output == "json" {
				return writeStatusJSON(out, lines, appliedTotal, pendingTotal)
			}
			if multiSource {
				fmt.Fprintf(out, "%d migration(s) applied, %d pending\n", appliedTotal, pendingTotal)
				// Reuses the same `multiSource` gate as the stderr desync
				// warning above on purpose: a per-source skip branch must
				// not let the two diverge (a desync gets a source warned
				// twice or not at all).
				for _, l := range lines {
					if l.downgraded {
						fmt.Fprintf(out, "  %s: %d applied, %d pending (db reports v%d%s — binary out of date)\n",
							l.src.Name, l.applied, l.pending, l.dbVersion, dirtyTag(l.dirty))
					} else {
						fmt.Fprintf(out, "  %s: %d applied (at v%d%s), %d pending\n",
							l.src.Name, l.applied, l.dbVersion, dirtyTag(l.dirty), l.pending)
					}
				}
			} else {
				// Single-source: fold the version into the headline so
				// operators needn't run `version` separately. dbVersion is
				// the raw tracking-table value (matches `force V`).
				l := lines[0]
				fmt.Fprintf(out, "%d migration(s) applied (at v%d%s), %d pending\n",
					l.applied, l.dbVersion, dirtyTag(l.dirty), l.pending)
			}
			return nil
		},
	}
	// No -o shorthand: the kagent root command already owns -o
	// (--output-format) as a persistent flag, and cobra panics on a
	// shorthand redefinition.
	cmd.Flags().StringVar(&output, "output", "text",
		`Output format: "text" (default) or "json"`)
	return cmd
}

// statusJSON is the wire format for `kagent db migrate status -o json`.
// Operators consume it via `jq`, so the field names and types are a frozen
// contract; TestStatusJSONShape locks them and fails CI on a rename or
// retype.
type statusJSON struct {
	Applied int                `json:"applied"`
	Pending int                `json:"pending"`
	Sources []statusSourceJSON `json:"sources"`
}

// statusSourceJSON is the per-source object inside statusJSON; same
// frozen-shape contract, also locked by TestStatusJSONShape.
type statusSourceJSON struct {
	Name       string `json:"name"`
	Applied    int    `json:"applied"`
	Pending    int    `json:"pending"`
	Version    int    `json:"version"`
	Downgraded bool   `json:"downgraded"`
	Dirty      bool   `json:"dirty"`
}

func writeStatusJSON(out io.Writer, lines []lineRow, appliedTotal, pendingTotal int) error {
	payload := statusJSON{
		Applied: appliedTotal,
		Pending: pendingTotal,
		Sources: make([]statusSourceJSON, 0, len(lines)),
	}
	for _, l := range lines {
		payload.Sources = append(payload.Sources, statusSourceJSON{
			Name:       l.src.Name,
			Applied:    l.applied,
			Pending:    l.pending,
			Version:    l.dbVersion,
			Downgraded: l.downgraded,
			Dirty:      l.dirty,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// dirtyTag returns " (dirty)" when the source is mid-failed-migration, ""
// otherwise. Used to annotate the version in status/version text output
// without adding a separate line.
func dirtyTag(dirty bool) string {
	if dirty {
		return " (dirty)"
	}
	return ""
}

// versionAnnotation renders the trailing annotation for `version` output.
// Disambiguates an unapplied-migrations state (v=0, !dirty) from a versioned
// state by tagging the former; dirty wins over the "no migrations applied"
// tag because it's the more actionable signal.
func versionAnnotation(v uint, dirty bool) string {
	if dirty {
		return " (dirty)"
	}
	if v == 0 {
		return " (no migrations applied)"
	}
	return ""
}

func newVersionCmd(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the highest applied migration version",
		Long: `Print the highest applied migration version.
For a single registered source the value is on one line; multi-source
binaries print one line per source. When multiple sources are
registered, --source filters to a single track.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dsn, err := state.resolveDSN()
			if err != nil {
				return err
			}
			srcs := state.sources
			if len(srcs) == 0 {
				return errors.New("no migration sources registered")
			}
			// --source filters the output even though version is otherwise
			// an aggregate op. Empty flag = print all.
			if state.source != "" {
				picked := -1
				for i, s := range srcs {
					if s.Name == state.source {
						picked = i
						break
					}
				}
				if picked < 0 {
					return fmt.Errorf("--source %q not registered; registered sources: %s", state.source, sourceNames(srcs))
				}
				srcs = []migrations.Source{srcs[picked]}
			}
			out := cmd.OutOrStdout()
			if len(srcs) == 1 {
				return migrations.WithMigrator(cmd.Context(), dsn, srcs[0], func(mg *migrate.Migrate) error {
					v, dirty, err := readVersion(mg)
					if err != nil {
						return err
					}
					fmt.Fprintf(out, "%d%s\n", v, versionAnnotation(v, dirty))
					return nil
				})
			}
			for _, src := range srcs {
				if err := migrations.WithMigrator(cmd.Context(), dsn, src, func(mg *migrate.Migrate) error {
					v, dirty, err := readVersion(mg)
					if err != nil {
						return err
					}
					fmt.Fprintf(out, "%s: %d%s\n", src.Name, v, versionAnnotation(v, dirty))
					return nil
				}); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func newGotoCmd(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use:   "goto V",
		Short: "Move the selected source's schema to version V",
		Long: `Move the selected source's schema to version V (forward or backward).
V=0 is the special "empty schema" target: every applied migration in
the source is rolled back.

Refuses to run while the source's tracking table is dirty; clear it
with 'force' first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := strconv.Atoi(args[0])
			if err != nil || v < 0 {
				return fmt.Errorf("expected a non-negative integer for V, got %q", args[0])
			}
			dsn, err := state.resolveDSN()
			if err != nil {
				return err
			}
			src, err := state.resolveSource()
			if err != nil {
				return err
			}
			return migrations.WithMigrator(cmd.Context(), dsn, src, func(mg *migrate.Migrate) error {
				if err := ensureClean(src, mg); err != nil {
					return err
				}
				if v == 0 {
					if err := mg.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), "schema is at version 0 (empty)")
					return nil
				}
				if err := mg.Migrate(uint(v)); err != nil && !errors.Is(err, migrate.ErrNoChange) {
					return err
				}
				actual, dirty, aerr := readVersion(mg)
				if aerr != nil {
					return aerr
				}
				fmt.Fprintf(cmd.OutOrStdout(), "schema is at version %d%s\n", actual, versionAnnotation(actual, dirty))
				return nil
			})
		},
	}
}

func newForceCmd(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use:   "force V",
		Short: "Mark version V as applied without running its SQL",
		Long: `Used to reconcile the selected source's tracking table after manual
remediation, e.g. to clear a dirty flag left by a failed migration.
V=0 clears the version record entirely (the "no migrations applied"
state). Any other V must correspond to a shipped migration file in
the selected source — otherwise the tracking row would point at a
version the binary cannot apply or roll back to, wedging the DB.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := strconv.Atoi(args[0])
			if err != nil || v < 0 {
				return fmt.Errorf("expected a non-negative integer for V, got %q", args[0])
			}
			dsn, err := state.resolveDSN()
			if err != nil {
				return err
			}
			src, err := state.resolveSource()
			if err != nil {
				return err
			}
			if v > 0 {
				versions, err := sourceFileVersions(src)
				if err != nil {
					return err
				}
				if !slices.Contains(versions, v) {
					return fmt.Errorf(
						"version %d is not a shipped migration for source %q; valid versions are %s",
						v, src.Name, formatVersionList(versions))
				}
			}
			return migrations.WithMigrator(cmd.Context(), dsn, src, func(mg *migrate.Migrate) error {
				if v == 0 {
					// golang-migrate's Force takes -1 to delete the version
					// record; 0 is not a valid stored version.
					if err := mg.Force(-1); err != nil {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), "version record cleared (no migrations applied)")
					return nil
				}
				if err := mg.Force(v); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "version %d marked as applied\n", v)
				return nil
			})
		},
	}
}

// countVersionsBetween returns the count of shipped source migrations in the
// half-open interval (low, high]. Used by `down N` to report the actual
// number of migrations rolled back regardless of whether the user-supplied N
// exceeded the applied count. Returns 0 on a source-enumeration error; the
// call site already surfaces success/failure via Steps(-N) and the count is
// operator-facing display only.
func countVersionsBetween(src migrations.Source, low, high uint) int {
	versions, err := sourceFileVersions(src)
	if err != nil {
		return 0
	}
	count := 0
	for _, v := range versions {
		uv := uint(v)
		if uv > low && uv <= high {
			count++
		}
	}
	return count
}

// formatVersionList renders a small []int as a human-readable list for error
// messages: "1, 2, 5" or "(none)" if empty.
func formatVersionList(versions []int) string {
	if len(versions) == 0 {
		return "(none)"
	}
	parts := make([]string, len(versions))
	for i, v := range versions {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ", ")
}

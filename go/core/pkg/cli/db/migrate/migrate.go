// Package migrate provides the database migration commands.
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

	"github.com/pressly/goose/v3"
	"github.com/spf13/cobra"

	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

const (
	dbURLEnv   = "POSTGRES_DATABASE_URL"
	sourceFlag = "source"
)

var (
	sourceNameRE    = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	migrationFileRE = regexp.MustCompile(`^([0-9]+)_.+\.sql$`)
)

// SourcesFunc gets the migration sources for one command.
type SourcesFunc func(ctx context.Context) ([]migrations.Source, error)

type commandState struct {
	dbURL     string
	source    string
	resolveFn SourcesFunc

	resolved   bool
	sources    []migrations.Source
	sourcesErr error
}

func (s *commandState) getSources(ctx context.Context) ([]migrations.Source, error) {
	if s.resolved {
		return s.sources, s.sourcesErr
	}
	s.resolved = true
	sources, err := s.resolveFn(ctx)
	if err != nil {
		s.sourcesErr = fmt.Errorf("resolve migration sources: %w", err)
		return nil, s.sourcesErr
	}
	if err := validateSourceNames(sources); err != nil {
		s.sourcesErr = err
		return nil, err
	}
	s.sources = append([]migrations.Source(nil), sources...)
	return s.sources, nil
}

func validateSourceNames(sources []migrations.Source) error {
	seen := make(map[string]bool, len(sources))
	for _, source := range sources {
		if !sourceNameRE.MatchString(source.Name) {
			return fmt.Errorf("migration source name %q must match %s", source.Name, sourceNameRE.String())
		}
		if seen[source.Name] {
			return fmt.Errorf("migration source %q appears twice", source.Name)
		}
		seen[source.Name] = true
	}
	return nil
}

// NewCommand returns the migrate command.
func NewCommand(sources ...migrations.Source) *cobra.Command {
	if err := validateSourceNames(sources); err != nil {
		panic("migrate.NewCommand: " + err.Error())
	}
	static := append([]migrations.Source(nil), sources...)
	return NewCommandFromFunc(func(context.Context) ([]migrations.Source, error) {
		return static, nil
	})
}

// NewCommandFromFunc returns a command with deferred source resolution.
func NewCommandFromFunc(fn SourcesFunc) *cobra.Command {
	state := &commandState{resolveFn: fn}
	command := &cobra.Command{
		Use:   "migrate",
		Short: "Apply, roll back, and inspect database migrations",
		Long: `Apply, roll back, and inspect database migrations.
The command reads POSTGRES_DATABASE_URL when --db-url is empty.`,
	}
	command.PersistentFlags().StringVar(&state.dbURL, "db-url", "", "PostgreSQL connection URL")
	command.PersistentFlags().StringVar(&state.source, sourceFlag, "", "Migration source for down, goto, or version")
	command.AddCommand(newUpCmd(state))
	command.AddCommand(newDownCmd(state))
	command.AddCommand(newStatusCmd(state))
	command.AddCommand(newVersionCmd(state))
	command.AddCommand(newGotoCmd(state))
	return command
}

func (s *commandState) resolveDSN() (string, error) {
	dsn := strings.TrimSpace(s.dbURL)
	if dsn == "" {
		dsn = os.Getenv(dbURLEnv)
	}
	if dsn == "" {
		return "", fmt.Errorf("set the database URL with --db-url or %s", dbURLEnv)
	}
	return dsn, nil
}

func (s *commandState) resolveSource(ctx context.Context) (migrations.Source, error) {
	sources, err := s.getSources(ctx)
	if err != nil {
		return migrations.Source{}, err
	}
	if len(sources) == 0 {
		return migrations.Source{}, errors.New("no migration sources are registered")
	}
	if len(sources) == 1 {
		if s.source != "" && s.source != sources[0].Name {
			return migrations.Source{}, fmt.Errorf("source %q is not registered", s.source)
		}
		return sources[0], nil
	}
	if s.source == "" {
		return migrations.Source{}, fmt.Errorf("registered sources: %s. Pass --source", sourceNames(sources))
	}
	for _, source := range sources {
		if source.Name == s.source {
			return source, nil
		}
	}
	return migrations.Source{}, fmt.Errorf("source %q is not registered", s.source)
}

func sourceNames(sources []migrations.Source) string {
	names := make([]string, len(sources))
	for i, source := range sources {
		names[i] = source.Name
	}
	return strings.Join(names, ", ")
}

func sourceFileVersions(source migrations.Source) ([]int64, error) {
	entries, err := fs.ReadDir(source.FS, source.Dir)
	if err != nil {
		return nil, fmt.Errorf("read migration directory %s: %w", source.Dir, err)
	}
	versions := make([]int64, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		match := migrationFileRE.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil, fmt.Errorf("invalid migration file %s", entry.Name())
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("invalid migration version in %s", entry.Name())
		}
		versions = append(versions, version)
	}
	slices.Sort(versions)
	return versions, nil
}

func readVersion(ctx context.Context, provider *goose.Provider) (int64, error) {
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("read migration version: %w", err)
	}
	return version, nil
}

func newUpCmd(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if state.source != "" {
				return errors.New("up applies all sources. --source is not applicable")
			}
			dsn, err := state.resolveDSN()
			if err != nil {
				return err
			}
			sources, err := state.getSources(command.Context())
			if err != nil {
				return err
			}
			if len(sources) == 0 {
				return errors.New("no migration sources are registered")
			}
			if err := migrations.RunUp(command.Context(), dsn, sources); err != nil {
				return err
			}
			fmt.Fprintln(command.OutOrStdout(), "schema is up to date")
			return nil
		},
	}
}

func newDownCmd(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use:   "down N",
		Short: "Roll back the latest N migrations",
		Long:  "Roll back the latest N migrations. A down migration can delete data.",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			count, err := strconv.Atoi(args[0])
			if err != nil || count < 1 {
				return fmt.Errorf("n must be a positive integer, got %q", args[0])
			}
			dsn, err := state.resolveDSN()
			if err != nil {
				return err
			}
			source, err := state.resolveSource(command.Context())
			if err != nil {
				return err
			}
			versions, err := sourceFileVersions(source)
			if err != nil {
				return err
			}
			return migrations.WithProvider(command.Context(), dsn, source, func(provider *goose.Provider) error {
				current, err := readVersion(command.Context(), provider)
				if err != nil {
					return err
				}
				if current > versions[len(versions)-1] {
					return fmt.Errorf("database version %d exceeds embedded version %d", current, versions[len(versions)-1])
				}
				status, err := provider.Status(command.Context())
				if err != nil {
					return err
				}
				applied := make([]int64, 0, len(status))
				for _, migration := range status {
					if migration.State == goose.StateApplied {
						applied = append(applied, migration.Source.Version)
					}
				}
				if len(applied) == 0 {
					fmt.Fprintln(command.OutOrStdout(), "no migrations to roll back")
					return nil
				}
				targetIndex := len(applied) - count - 1
				target := int64(0)
				if targetIndex >= 0 {
					target = applied[targetIndex]
				}
				results, err := provider.DownTo(command.Context(), target)
				if err != nil {
					return err
				}
				fmt.Fprintf(command.OutOrStdout(), "rolled back %d migration(s)\n", len(results))
				return nil
			})
		},
	}
}

type lineRow struct {
	source    migrations.Source
	applied   int
	pending   int
	dbVersion int64
	ahead     bool
}

func newStatusCmd(state *commandState) *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if state.source != "" {
				return errors.New("status shows all sources. --source is not applicable")
			}
			if output != "text" && output != "json" {
				return fmt.Errorf("invalid --output %q. Use text or json", output)
			}
			dsn, err := state.resolveDSN()
			if err != nil {
				return err
			}
			sources, err := state.getSources(command.Context())
			if err != nil {
				return err
			}
			if len(sources) == 0 {
				return errors.New("no migration sources are registered")
			}

			lines := make([]lineRow, 0, len(sources))
			appliedTotal := 0
			pendingTotal := 0
			for _, source := range sources {
				line := lineRow{source: source}
				versions, err := sourceFileVersions(source)
				if err != nil {
					return err
				}
				err = migrations.WithProvider(command.Context(), dsn, source, func(provider *goose.Provider) error {
					status, err := provider.Status(command.Context())
					if err != nil {
						return err
					}
					for _, migration := range status {
						if migration.State == goose.StateApplied {
							line.applied++
						} else {
							line.pending++
						}
					}
					line.dbVersion, err = readVersion(command.Context(), provider)
					return err
				})
				if err != nil {
					return err
				}
				if len(versions) > 0 {
					line.ahead = line.dbVersion > versions[len(versions)-1]
				}
				lines = append(lines, line)
				appliedTotal += line.applied
				pendingTotal += line.pending
			}

			if output == "json" {
				return writeStatusJSON(command.OutOrStdout(), lines, appliedTotal, pendingTotal)
			}
			writeStatusText(command.OutOrStdout(), lines, appliedTotal, pendingTotal)
			return nil
		},
	}
	command.Flags().StringVar(&output, "output", "text", `Output format: "text" or "json"`)
	return command
}

func writeStatusText(out io.Writer, lines []lineRow, appliedTotal, pendingTotal int) {
	if len(lines) == 1 {
		line := lines[0]
		fmt.Fprintf(out, "%d migration(s) applied (at v%d), %d pending\n", line.applied, line.dbVersion, line.pending)
		return
	}
	fmt.Fprintf(out, "%d migration(s) applied, %d pending\n", appliedTotal, pendingTotal)
	for _, line := range lines {
		if line.ahead {
			fmt.Fprintf(out, "  %s: %d applied, %d pending (database reports v%d. The binary is old)\n",
				line.source.Name, line.applied, line.pending, line.dbVersion)
			continue
		}
		fmt.Fprintf(out, "  %s: %d applied (at v%d), %d pending\n",
			line.source.Name, line.applied, line.dbVersion, line.pending)
	}
}

type statusJSON struct {
	Applied int                `json:"applied"`
	Pending int                `json:"pending"`
	Sources []statusSourceJSON `json:"sources"`
}

type statusSourceJSON struct {
	Name       string `json:"name"`
	Applied    int    `json:"applied"`
	Pending    int    `json:"pending"`
	Version    int64  `json:"version"`
	Downgraded bool   `json:"downgraded"`
}

func writeStatusJSON(out io.Writer, lines []lineRow, appliedTotal, pendingTotal int) error {
	payload := statusJSON{
		Applied: appliedTotal,
		Pending: pendingTotal,
		Sources: make([]statusSourceJSON, 0, len(lines)),
	}
	for _, line := range lines {
		payload.Sources = append(payload.Sources, statusSourceJSON{
			Name:       line.source.Name,
			Applied:    line.applied,
			Pending:    line.pending,
			Version:    line.dbVersion,
			Downgraded: line.ahead,
		})
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func newVersionCmd(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the applied migration version",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			dsn, err := state.resolveDSN()
			if err != nil {
				return err
			}
			sources, err := state.getSources(command.Context())
			if err != nil {
				return err
			}
			if len(sources) == 0 {
				return errors.New("no migration sources are registered")
			}
			if state.source != "" {
				index := slices.IndexFunc(sources, func(source migrations.Source) bool {
					return source.Name == state.source
				})
				if index < 0 {
					return fmt.Errorf("source %q is not registered", state.source)
				}
				sources = sources[index : index+1]
			}
			for _, source := range sources {
				err := migrations.WithProvider(command.Context(), dsn, source, func(provider *goose.Provider) error {
					version, err := readVersion(command.Context(), provider)
					if err != nil {
						return err
					}
					if len(sources) == 1 {
						fmt.Fprintf(command.OutOrStdout(), "%d%s\n", version, versionAnnotation(version))
					} else {
						fmt.Fprintf(command.OutOrStdout(), "%s: %d%s\n", source.Name, version, versionAnnotation(version))
					}
					return nil
				})
				if err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func versionAnnotation(version int64) string {
	if version == 0 {
		return " (no migrations applied)"
	}
	return ""
}

func newGotoCmd(state *commandState) *cobra.Command {
	return &cobra.Command{
		Use:   "goto V",
		Short: "Move one source to version V",
		Long:  "Move one source to version V. Version zero removes its schema.",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			target, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || target < 0 {
				return fmt.Errorf("v must be a non-negative integer, got %q", args[0])
			}
			dsn, err := state.resolveDSN()
			if err != nil {
				return err
			}
			source, err := state.resolveSource(command.Context())
			if err != nil {
				return err
			}
			versions, err := sourceFileVersions(source)
			if err != nil {
				return err
			}
			if target != 0 && !slices.Contains(versions, target) {
				return fmt.Errorf("version %d is not available. Valid versions are %s", target, formatVersionList(versions))
			}
			return migrations.WithProvider(command.Context(), dsn, source, func(provider *goose.Provider) error {
				current, err := readVersion(command.Context(), provider)
				if err != nil {
					return err
				}
				if current > versions[len(versions)-1] {
					return fmt.Errorf("database version %d exceeds embedded version %d", current, versions[len(versions)-1])
				}
				if target > current {
					_, err = provider.UpTo(command.Context(), target)
				} else if target < current {
					_, err = provider.DownTo(command.Context(), target)
				}
				if err != nil {
					return err
				}
				fmt.Fprintf(command.OutOrStdout(), "schema is at version %d%s\n", target, versionAnnotation(target))
				return nil
			})
		},
	}
}

func formatVersionList(versions []int64) string {
	if len(versions) == 0 {
		return "(none)"
	}
	parts := make([]string, len(versions))
	for i, version := range versions {
		parts[i] = strconv.FormatInt(version, 10)
	}
	return strings.Join(parts, ", ")
}

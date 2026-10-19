package migrations

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/kagent-dev/kagent/go/core/pkg/consts"
)

const (
	advisoryLockIDSalt  uint32 = 1486364155
	coreTrackingTable          = "schema_migrations"
	vectorTrackingTable        = "vector_schema_migrations"
)

var (
	identifierRE    = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
	migrationFileRE = regexp.MustCompile(`^([0-9]+)_.+\.sql$`)
)

// Source describes one migration source. Schema is required: the source's
// tables and tracking table live there.
type Source struct {
	Name          string
	Schema        string
	VectorSchema  string
	TrackingTable string
	FS            fs.FS
	Dir           string
	// PreCheck runs before any source migrates, on a pool that uses the
	// caller's configuration rather than this source's search path.
	PreCheck func(ctx context.Context, pool *pgxpool.Pool) error
}

// BuiltinSources returns the built-in migration sources in the default schemas.
func BuiltinSources(vectorEnabled bool) []Source {
	return BuiltinSourcesInSchema(vectorEnabled, consts.DefaultPostgresTableSchema, consts.DefaultPgvectorSchema)
}

// BuiltinSourcesInSchema returns the built-in sources with their table and
// pgvector schemas selected independently. Empty values use the defaults.
func BuiltinSourcesInSchema(vectorEnabled bool, schema, vectorSchema string) []Source {
	if schema == "" {
		schema = consts.DefaultPostgresTableSchema
	}
	if vectorSchema == "" {
		vectorSchema = consts.DefaultPgvectorSchema
	}
	sources := []Source{{
		Name:          "core",
		Schema:        schema,
		TrackingTable: coreTrackingTable,
		FS:            FS,
		Dir:           "core",
	}}
	if vectorEnabled {
		sources[0].VectorSchema = vectorSchema
		sources = append(sources, Source{
			Name:          "vector",
			Schema:        schema,
			VectorSchema:  vectorSchema,
			TrackingTable: vectorTrackingTable,
			FS:            FS,
			Dir:           "vector",
			PreCheck:      pgvectorPreCheck(vectorSchema),
		})
	}
	return sources
}

// RunUp applies all pending migrations in source order. Each source migrates
// on its own pool built from config, with the source's schema as its search path.
func RunUp(ctx context.Context, config *pgxpool.Config, sources []Source) error {
	if len(sources) == 0 {
		return nil
	}
	if err := validateSources(sources); err != nil {
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	err = preflight(ctx, pool, sources)
	pool.Close()
	if err != nil {
		return err
	}

	for _, src := range sources {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cancel before %s migrations: %w", src.Name, err)
		}
		err := WithProvider(ctx, config, src, func(provider *goose.Provider) error {
			_, err := provider.Up(ctx)
			return err
		})
		if err != nil {
			return fmt.Errorf("%s migrations: %w", src.Name, err)
		}
	}
	return nil
}

// VerifyMigrated checks migration state without database writes.
func VerifyMigrated(ctx context.Context, config *pgxpool.Config, sources []Source) error {
	if len(sources) == 0 {
		return nil
	}
	if err := validateSources(sources); err != nil {
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	if err := preflight(ctx, pool, sources); err != nil {
		return err
	}

	for _, src := range sources {
		versions, err := Versions(src)
		if err != nil {
			return fmt.Errorf("read %s migrations: %w", src.Name, err)
		}
		table := quoteIdentifier(src.Schema) + "." + quoteIdentifier(src.TrackingTable)
		exists, err := tableExists(ctx, pool, table)
		if err != nil {
			return fmt.Errorf("check %s migration table: %w", src.Name, err)
		}
		if !exists {
			return fmt.Errorf("source %s has no migration table", src.Name)
		}

		rows, err := pool.Query(ctx, "SELECT version_id, is_applied FROM "+table)
		if err != nil {
			return fmt.Errorf("read %s migration table: %w", src.Name, err)
		}
		applied := make(map[int64]bool)
		var version int64
		var isApplied bool
		if _, err := pgx.ForEachRow(rows, []any{&version, &isApplied}, func() error {
			if version > 0 && isApplied {
				applied[version] = true
			}
			return nil
		}); err != nil {
			return fmt.Errorf("read %s migration rows: %w", src.Name, err)
		}

		embedded := make(map[int64]bool, len(versions))
		for _, version := range versions {
			embedded[version] = true
			if !applied[version] {
				return fmt.Errorf("source %s needs migration version %d", src.Name, version)
			}
		}
		latest := versions[len(versions)-1]
		for version := range applied {
			if version <= latest && !embedded[version] {
				return fmt.Errorf("source %s has unknown version %d", src.Name, version)
			}
		}
	}
	return nil
}

// preflight runs every source's precheck before any source migrates.
func preflight(ctx context.Context, pool *pgxpool.Pool, sources []Source) error {
	for _, src := range sources {
		if src.PreCheck == nil {
			continue
		}
		if err := src.PreCheck(ctx, pool); err != nil {
			return fmt.Errorf("%s precheck: %w", src.Name, err)
		}
	}
	return nil
}

// WithProvider runs fn while one source lock is held.
func WithProvider(ctx context.Context, config *pgxpool.Config, src Source, fn func(*goose.Provider) error) (retErr error) {
	if err := validateSources([]Source{src}); err != nil {
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, sourceConfig(config, src))
	if err != nil {
		return fmt.Errorf("open database for %s: %w", src.Name, err)
	}
	defer pool.Close()

	if err := ensureSchema(ctx, pool, src.Schema); err != nil {
		return err
	}

	var databaseName, schemaName string
	if err := pool.QueryRow(ctx, "SELECT current_database(), COALESCE(current_schema(), '')").Scan(&databaseName, &schemaName); err != nil {
		return fmt.Errorf("resolve database identity: %w", err)
	}
	if schemaName != src.Schema {
		return fmt.Errorf("migration schema %q is not accessible (current schema is %q)", src.Schema, schemaName)
	}
	if err := rejectOldTrackingTable(ctx, pool, schemaName, src); err != nil {
		return err
	}

	db := stdlib.OpenDBFromPool(pool)
	defer func() {
		retErr = errors.Join(retErr, db.Close())
	}()
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(advisoryLockID(databaseName, schemaName, src.TrackingTable)))
	if err != nil {
		return fmt.Errorf("create %s migration lock: %w", src.Name, err)
	}
	lockConn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open %s lock connection: %w", src.Name, err)
	}
	defer func() {
		retErr = errors.Join(retErr, lockConn.Close())
	}()
	if err := locker.SessionLock(ctx, lockConn); err != nil {
		return fmt.Errorf("lock %s migrations: %w", src.Name, err)
	}
	defer func() {
		if err := locker.SessionUnlock(context.WithoutCancel(ctx), lockConn); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("unlock %s migrations: %w", src.Name, err))
		}
	}()

	sourceFS, err := fs.Sub(src.FS, src.Dir)
	if err != nil {
		return fmt.Errorf("open migration directory %s: %w", src.Dir, err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		sourceFS,
		goose.WithTableName(src.TrackingTable),
		goose.WithDisableGlobalRegistry(true),
		goose.WithLogger(goose.NopLogger()),
	)
	if err != nil {
		return fmt.Errorf("create %s migration provider: %w", src.Name, err)
	}
	return fn(provider)
}

// sourceConfig returns a copy of config for one source's migrations. The
// vector migration reads its pgvector schema from kagent.vector_schema.
func sourceConfig(config *pgxpool.Config, src Source) *pgxpool.Config {
	config = config.Copy()
	// One connection holds the migration session lock while goose uses the other.
	config.MaxConns = 2
	config.MinConns = 0
	config.MinIdleConns = 0
	config.ConnConfig.RuntimeParams["search_path"] = pgx.Identifier{src.Schema}.Sanitize()
	if src.VectorSchema != "" {
		config.ConnConfig.RuntimeParams["kagent.vector_schema"] = src.VectorSchema
	}
	return config
}

func rejectOldTrackingTable(ctx context.Context, pool *pgxpool.Pool, schema string, src Source) error {
	var old bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = $2 AND column_name = 'dirty'
		)`, schema, src.TrackingTable).Scan(&old)
	if err != nil {
		return fmt.Errorf("check %s migration table: %w", src.Name, err)
	}
	if old {
		return fmt.Errorf("source %s uses an unsupported migration table. Use a new PostgreSQL database", src.Name)
	}
	return nil
}

// ensureSchema creates schema when it is missing and the role may. An existing
// schema needs no CREATE privilege on the database. The advisory lock matches
// Substrate's, so concurrent starts do not race CREATE SCHEMA.
func ensureSchema(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "kagent:create-schema:"+schema); err != nil {
			return fmt.Errorf("lock schema %s: %w", schema, err)
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)", schema).Scan(&exists); err != nil {
			return fmt.Errorf("check schema %s: %w", schema, err)
		}
		if exists {
			return nil
		}
		if _, err := tx.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+quoteIdentifier(schema)); err != nil {
			return fmt.Errorf("create schema %s: %w", schema, err)
		}
		return nil
	})
}

func validateSources(sources []Source) error {
	seen := make(map[string]string, len(sources))
	for _, src := range sources {
		switch {
		case src.Name == "":
			return errors.New("migration source has an empty name")
		case src.TrackingTable == "":
			return fmt.Errorf("source %s has an empty tracking table", src.Name)
		case src.FS == nil:
			return fmt.Errorf("source %s has no migration filesystem", src.Name)
		case src.Dir == "":
			return fmt.Errorf("source %s has an empty migration directory", src.Name)
		case src.Schema == "":
			return fmt.Errorf("source %s has an empty schema", src.Name)
		}
		if err := validateIdentifier("schema", src.Schema); err != nil {
			return fmt.Errorf("source %s: %w", src.Name, err)
		}
		if src.VectorSchema != "" {
			if err := validateIdentifier("pgvector schema", src.VectorSchema); err != nil {
				return fmt.Errorf("source %s: %w", src.Name, err)
			}
		}
		if err := validateIdentifier("tracking table", src.TrackingTable); err != nil {
			return fmt.Errorf("source %s: %w", src.Name, err)
		}
		if _, err := Versions(src); err != nil {
			return fmt.Errorf("source %s: %w", src.Name, err)
		}
		key := src.Schema + "\x00" + src.TrackingTable
		if other, ok := seen[key]; ok {
			return fmt.Errorf("sources %s and %s share one tracking table", other, src.Name)
		}
		seen[key] = src.Name
	}
	return nil
}

// Versions returns the source's migration versions in ascending order. It
// rejects an empty source and files Goose would misread or that disable transactions.
func Versions(src Source) ([]int64, error) {
	entries, err := fs.ReadDir(src.FS, src.Dir)
	if err != nil {
		return nil, fmt.Errorf("read migration directory %s: %w", src.Dir, err)
	}
	versions := make([]int64, 0, len(entries))
	seen := make(map[int64]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".up.sql") || strings.HasSuffix(entry.Name(), ".down.sql") {
			return nil, fmt.Errorf("invalid migration file %s", entry.Name())
		}
		match := migrationFileRE.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil, fmt.Errorf("invalid migration file %s", entry.Name())
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("invalid migration version in %s", entry.Name())
		}
		if other, ok := seen[version]; ok {
			return nil, fmt.Errorf("files %s and %s use version %d", other, entry.Name(), version)
		}
		body, err := fs.ReadFile(src.FS, path.Join(src.Dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration file %s: %w", entry.Name(), err)
		}
		var hasUp, hasDown bool
		for line := range strings.SplitSeq(string(body), "\n") {
			switch gooseAnnotation(line) {
			case "no transaction":
				return nil, fmt.Errorf("migration %s disables transactions", entry.Name())
			case "up":
				hasUp = true
			case "down":
				hasDown = true
			}
		}
		if !hasUp {
			return nil, fmt.Errorf("migration %s has no Goose Up section", entry.Name())
		}
		if !hasDown {
			return nil, fmt.Errorf("migration %s has no Goose Down section", entry.Name())
		}
		seen[version] = entry.Name()
		versions = append(versions, version)
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("directory %s has no migrations", src.Dir)
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("migration directory %s has no migrations", src.Dir)
	}
	slices.Sort(versions)
	return versions, nil
}

func gooseAnnotation(line string) string {
	if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) ||
		!strings.HasPrefix(strings.TrimSpace(line), "--") || !strings.Contains(line, "+goose") {
		return ""
	}
	command := strings.ReplaceAll(line, "--", "")
	command = strings.Replace(command, "+goose", "", 1)
	if strings.Contains(command, "+goose") {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(command))
}

func pgvectorPreCheck(expectedSchema string) func(context.Context, *pgxpool.Pool) error {
	return func(ctx context.Context, pool *pgxpool.Pool) error {
		var schema string
		err := pool.QueryRow(ctx, `SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = 'vector'`).Scan(&schema)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("pgvector is not installed in schema %q", expectedSchema)
		}
		if err != nil {
			return fmt.Errorf("check pgvector schema: %w", err)
		}
		if schema != expectedSchema {
			return fmt.Errorf("pgvector is installed in schema %q, expected %q", schema, expectedSchema)
		}
		return nil
	}
}

func validateIdentifier(kind, value string) error {
	if len(value) == 0 || len(value) > 63 {
		return fmt.Errorf("%s %q must contain 1 to 63 characters", kind, value)
	}
	if !identifierRE.MatchString(value) {
		return fmt.Errorf("%s %q must match %s", kind, value, identifierRE.String())
	}
	return nil
}

func tableExists(ctx context.Context, pool *pgxpool.Pool, table string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists)
	return exists, err
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func advisoryLockID(database, schema, table string) int64 {
	name := strings.Join([]string{schema, table, database}, "\x00")
	sum := crc32.ChecksumIEEE([]byte(name))
	return int64(sum * advisoryLockIDSalt)
}

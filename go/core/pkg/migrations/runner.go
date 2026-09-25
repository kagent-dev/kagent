package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	nurl "net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
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

// Source describes one migration source.
type Source struct {
	Name          string
	Schema        string
	VectorSchema  string
	TrackingTable string
	FS            fs.FS
	Dir           string
	PreCheck      func(url string) error
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

// RunUp applies all pending migrations in source order.
func RunUp(ctx context.Context, url string, sources []Source) error {
	return RunUpAsRole(ctx, url, "", sources)
}

// RunUpAsRole applies all pending migrations after assuming role on every
// database connection. The authenticated login only needs membership in role.
func RunUpAsRole(ctx context.Context, url, role string, sources []Source) error {
	if len(sources) == 0 {
		return nil
	}
	if err := validateSources(sources); err != nil {
		return err
	}
	if err := checkResolvedSchemaCollisions(ctx, url, role, sources); err != nil {
		return err
	}

	for _, src := range sources {
		if src.PreCheck == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cancel before %s precheck: %w", src.Name, err)
		}
		if err := src.PreCheck(url); err != nil {
			return fmt.Errorf("%s precheck: %w", src.Name, err)
		}
	}

	for _, src := range sources {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cancel before %s migrations: %w", src.Name, err)
		}
		err := withProvider(ctx, url, role, src, func(provider *goose.Provider) error {
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
func VerifyMigrated(ctx context.Context, url string, sources []Source) error {
	return VerifyMigratedAsRole(ctx, url, "", sources)
}

// VerifyMigratedAsRole checks migration state after assuming role on every
// database connection.
func VerifyMigratedAsRole(ctx context.Context, url, role string, sources []Source) error {
	if len(sources) == 0 {
		return nil
	}
	if err := validateSources(sources); err != nil {
		return err
	}
	if err := checkResolvedSchemaCollisions(ctx, url, role, sources); err != nil {
		return err
	}
	for _, src := range sources {
		if src.PreCheck != nil {
			if err := src.PreCheck(url); err != nil {
				return fmt.Errorf("%s precheck: %w", src.Name, err)
			}
		}
	}

	db, err := openDB(url, role)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	for _, src := range sources {
		versions, err := migrationVersions(src)
		if err != nil {
			return fmt.Errorf("read %s migrations: %w", src.Name, err)
		}
		table := sourceTableName(src, src.TrackingTable)
		exists, err := tableExists(ctx, db, table)
		if err != nil {
			return fmt.Errorf("check %s migration table: %w", src.Name, err)
		}
		if !exists {
			return fmt.Errorf("source %s has no migration table", src.Name)
		}

		rows, err := db.QueryContext(ctx, "SELECT version_id, is_applied FROM "+table)
		if err != nil {
			return fmt.Errorf("read %s migration table: %w", src.Name, err)
		}
		applied := make(map[int64]bool)
		for rows.Next() {
			var version int64
			var isApplied bool
			if err := rows.Scan(&version, &isApplied); err != nil {
				rows.Close()
				return fmt.Errorf("scan %s migration table: %w", src.Name, err)
			}
			if version > 0 && isApplied {
				applied[version] = true
			}
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close %s migration rows: %w", src.Name, err)
		}
		if err := rows.Err(); err != nil {
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

// WithProvider runs fn while one source lock is held.
func WithProvider(ctx context.Context, url string, src Source, fn func(*goose.Provider) error) (retErr error) {
	return withProvider(ctx, url, "", src, fn)
}

// WithProviderAsRole runs fn while one source lock is held and every database
// connection has assumed role.
func WithProviderAsRole(ctx context.Context, url, role string, src Source, fn func(*goose.Provider) error) error {
	return withProvider(ctx, url, role, src, fn)
}

func withProvider(ctx context.Context, url, role string, src Source, fn func(*goose.Provider) error) (retErr error) {
	if err := validateSources([]Source{src}); err != nil {
		return err
	}
	connURL := url
	if src.Schema != "" || src.VectorSchema != "" {
		var err error
		connURL, err = withSearchPath(url, src.Schema, src.VectorSchema)
		if err != nil {
			return fmt.Errorf("set search path for %s: %w", src.Name, err)
		}
	}

	db, err := openDB(connURL, role)
	if err != nil {
		return fmt.Errorf("open database for %s: %w", src.Name, err)
	}
	defer func() {
		retErr = errors.Join(retErr, db.Close())
	}()

	if src.Schema != "" {
		var exists bool
		if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)", src.Schema).Scan(&exists); err != nil {
			return fmt.Errorf("check schema %s: %w", src.Schema, err)
		}
		if !exists {
			if _, err := db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+quoteIdentifier(src.Schema)); err != nil {
				return fmt.Errorf("create schema %s: %w", src.Schema, err)
			}
		}
	}

	var databaseName string
	var schemaName sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT current_database(), current_schema()").Scan(&databaseName, &schemaName); err != nil {
		return fmt.Errorf("resolve database identity: %w", err)
	}
	if src.Schema != "" && schemaName.String != src.Schema {
		return fmt.Errorf("migration schema %q is not accessible (current schema is %q)", src.Schema, schemaName.String)
	}
	if !schemaName.Valid {
		return errors.New("the connection has no current schema")
	}
	if err := rejectOldTrackingTable(ctx, db, schemaName.String, src); err != nil {
		return err
	}

	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(advisoryLockID(databaseName, schemaName.String, src.TrackingTable)))
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

func rejectOldTrackingTable(ctx context.Context, db *sql.DB, schema string, src Source) error {
	var old bool
	err := db.QueryRowContext(ctx, `
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
		}
		if src.Schema != "" {
			if err := validateIdentifier("schema", src.Schema); err != nil {
				return fmt.Errorf("source %s: %w", src.Name, err)
			}
		}
		if src.VectorSchema != "" {
			if err := validateIdentifier("pgvector schema", src.VectorSchema); err != nil {
				return fmt.Errorf("source %s: %w", src.Name, err)
			}
			if src.Schema == "" {
				return fmt.Errorf("source %s needs a table schema when pgvector is enabled", src.Name)
			}
		}
		if err := validateIdentifier("tracking table", src.TrackingTable); err != nil {
			return fmt.Errorf("source %s: %w", src.Name, err)
		}
		if _, err := migrationVersions(src); err != nil {
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

func migrationVersions(src Source) ([]int64, error) {
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

func checkResolvedSchemaCollisions(ctx context.Context, url, role string, sources []Source) error {
	var hasDefault, hasExplicit bool
	for _, src := range sources {
		if src.Schema == "" {
			hasDefault = true
		} else {
			hasExplicit = true
		}
	}
	if !hasDefault || !hasExplicit {
		return nil
	}

	db, err := openDB(url, role)
	if err != nil {
		return fmt.Errorf("open database to resolve schema: %w", err)
	}
	defer db.Close()
	var current sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&current); err != nil {
		return fmt.Errorf("resolve current schema: %w", err)
	}
	if !current.Valid {
		return nil
	}

	seen := make(map[string]string, len(sources))
	for _, src := range sources {
		schema := src.Schema
		if schema == "" {
			schema = current.String
		}
		key := schema + "\x00" + src.TrackingTable
		if other, ok := seen[key]; ok {
			return fmt.Errorf("sources %s and %s resolve to one tracking table", other, src.Name)
		}
		seen[key] = src.Name
	}
	return nil
}

func openDB(url, role string) (*sql.DB, error) {
	// Migrations share the application's DSN, including any pool-only options.
	poolConfig, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL connection string")
	}
	if role == "" {
		return stdlib.OpenDB(*poolConfig.ConnConfig), nil
	}
	return stdlib.OpenDB(*poolConfig.ConnConfig, stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, "SELECT set_config('role', $1, false)", role); err != nil {
			return fmt.Errorf("assuming PostgreSQL role %q: %w", role, err)
		}
		return nil
	})), nil
}

func pgvectorPreCheck(expectedSchema string) func(string) error {
	return func(url string) error {
		db, err := openDB(url, "")
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer db.Close()
		var schema string
		err = db.QueryRow(`SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = 'vector'`).Scan(&schema)
		if errors.Is(err, sql.ErrNoRows) {
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

func withSearchPath(dbURL, schema, vectorSchema string) (string, error) {
	u, err := nurl.Parse(dbURL)
	if err != nil {
		return "", fmt.Errorf("parse database URL: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("database URL has unsupported scheme %q", u.Scheme)
	}
	query := u.Query()
	if schema != "" {
		query.Set("search_path", pgx.Identifier{schema}.Sanitize())
	}
	if vectorSchema != "" {
		query.Set("kagent.vector_schema", vectorSchema)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
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

func sourceTableName(src Source, table string) string {
	if src.Schema == "" {
		return quoteIdentifier(table)
	}
	return quoteIdentifier(src.Schema) + "." + quoteIdentifier(table)
}

func tableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists)
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

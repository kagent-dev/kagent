package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// legacySchema is where Kagent kept its tables, its migration tracking and
// pgvector before they moved to configurable schemas.
const legacySchema = "public"

// ErrHalfMoved is the refusal to start on a database whose Kagent tables are
// split between public and the configured schema.
var ErrHalfMoved = errors.New("kagent's tables are split between two schemas")

var createRelationRE = regexp.MustCompile(`(?i)\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:UNIQUE\s+)?(?:MATERIALIZED\s+)?(TABLE|VIEW|SEQUENCE)\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)`)

// relation is one table, view or sequence Kagent's migrations create.
type relation struct {
	name string
	kind string // pg_class.relkind
}

// relocateFromPublic moves an installation's Kagent tables, views, sequences,
// migration tracking tables and pgvector from public into the configured
// schemas, so a database migrated before the schemas became configurable keeps
// its data. It runs in one transaction under the core migration lock: a
// database already in the configured schemas, or a fresh one, is left as it is,
// and one whose tables are split between both schemas is refused.
func relocateFromPublic(ctx context.Context, url, role string, sources []Source) (retErr error) {
	core, vector, ok := builtinSources(sources)
	if !ok {
		return nil
	}
	names, err := builtinRelationNames()
	if err != nil {
		return err
	}

	db, err := openDB(url, role)
	if err != nil {
		return fmt.Errorf("open database to move tables from %s: %w", legacySchema, err)
	}
	defer func() {
		retErr = errors.Join(retErr, db.Close())
	}()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin moving tables from %s: %w", legacySchema, err)
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, tx.Rollback())
		}
	}()

	var databaseName string
	if err := tx.QueryRowContext(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
		return fmt.Errorf("resolve database identity: %w", err)
	}
	// The core source's lock: a concurrent start waits here or in Goose, so
	// only one of them moves the tables and the other finds them moved.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", advisoryLockID(databaseName, core.Schema, coreTrackingTable)); err != nil {
		return fmt.Errorf("lock core migrations: %w", err)
	}

	legacy, target, err := placedRelations(ctx, tx, names, core.Schema)
	if err != nil {
		return err
	}
	if legacy == nil {
		return tx.Commit()
	}
	if len(target) > 0 {
		return halfMovedError(core.Schema, legacy, target)
	}

	if err := createSchema(ctx, tx, core.Schema); err != nil {
		return err
	}
	movedVector, err := moveVectorExtension(ctx, tx, vector)
	if err != nil {
		return err
	}
	// Tables first, which take their indexes, constraints and identity
	// sequences along; then views and the sequences no table owns.
	slices.SortStableFunc(legacy, func(a, b relation) int {
		return relationOrder(a.kind) - relationOrder(b.kind)
	})
	moved := 0
	for _, rel := range legacy {
		exists, err := relationExists(ctx, tx, legacySchema, rel.name)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		statement := fmt.Sprintf("ALTER %s %s.%s SET SCHEMA %s", relationKeyword(rel.kind),
			quoteIdentifier(legacySchema), quoteIdentifier(rel.name), quoteIdentifier(core.Schema))
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("move %s from %s to %s: %w", rel.name, legacySchema, core.Schema, err)
		}
		moved++
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit moving tables from %s: %w", legacySchema, err)
	}
	attrs := []any{"from", legacySchema, "to", core.Schema, "relations", moved}
	if movedVector {
		attrs = append(attrs, "pgvector_schema", vector.VectorSchema)
	}
	logging.FromContext(ctx).InfoContext(ctx, "moved kagent's tables to the configured schema", attrs...)
	return nil
}

// verifyNotInPublic refuses a database whose Kagent tables still wait in
// public: only a start that runs migrations moves them.
func verifyNotInPublic(ctx context.Context, url, role string, sources []Source) (retErr error) {
	core, _, ok := builtinSources(sources)
	if !ok {
		return nil
	}
	names, err := builtinRelationNames()
	if err != nil {
		return err
	}
	db, err := openDB(url, role)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		retErr = errors.Join(retErr, db.Close())
	}()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin layout check: %w", err)
	}
	defer func() {
		retErr = errors.Join(retErr, tx.Rollback())
	}()
	legacy, target, err := placedRelations(ctx, tx, names, core.Schema)
	if err != nil || legacy == nil {
		return err
	}
	if len(target) > 0 {
		return halfMovedError(core.Schema, legacy, target)
	}
	return fmt.Errorf("kagent's tables are in schema %s, not %s: start once with migrations enabled to move them", legacySchema, core.Schema)
}

// builtinSources returns the built-in core source, and the vector source when
// pgvector is enabled, when the core source is configured outside public.
func builtinSources(sources []Source) (core, vector Source, ok bool) {
	for _, src := range sources {
		if src.FS != FS {
			continue
		}
		switch src.TrackingTable {
		case coreTrackingTable:
			core, ok = src, true
		case vectorTrackingTable:
			vector = src
		}
	}
	if !ok || core.Schema == "" || core.Schema == legacySchema {
		return Source{}, Source{}, false
	}
	return core, vector, true
}

// builtinRelationNames returns the tables, views and sequences the built-in
// migrations create, and the tracking tables.
func builtinRelationNames() ([]string, error) {
	names := []string{coreTrackingTable, vectorTrackingTable}
	for _, dir := range []string{"core", "vector"} {
		entries, err := fs.ReadDir(FS, dir)
		if err != nil {
			return nil, fmt.Errorf("read migration directory %s: %w", dir, err)
		}
		for _, entry := range entries {
			body, err := fs.ReadFile(FS, dir+"/"+entry.Name())
			if err != nil {
				return nil, fmt.Errorf("read migration file %s: %w", entry.Name(), err)
			}
			for _, match := range createRelationRE.FindAllStringSubmatch(string(body), -1) {
				names = append(names, strings.ToLower(match[2]))
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

// placedRelations returns Kagent's relations in public and in schema. legacy is
// nil unless public holds the core tracking table of Goose: without it, public
// holds no Kagent installation to move.
func placedRelations(ctx context.Context, tx *sql.Tx, names []string, schema string) (legacy, target []relation, err error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT n.nspname, c.relname, c.relkind
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname IN ($1, $2) AND c.relname = ANY($3) AND c.relkind IN ('r', 'p', 'v', 'm', 'S')
		ORDER BY c.relname`, legacySchema, schema, names)
	if err != nil {
		return nil, nil, fmt.Errorf("list kagent's tables: %w", err)
	}
	defer rows.Close()
	tracked := false
	for rows.Next() {
		var namespace string
		var rel relation
		if err := rows.Scan(&namespace, &rel.name, &rel.kind); err != nil {
			return nil, nil, fmt.Errorf("scan kagent's tables: %w", err)
		}
		if namespace == schema {
			target = append(target, rel)
			continue
		}
		legacy = append(legacy, rel)
		tracked = tracked || rel.name == coreTrackingTable
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("list kagent's tables: %w", err)
	}
	if !tracked {
		return nil, target, nil
	}
	// A tracking table of golang-migrate belongs to the 1.x line, which the
	// legacy baseline check refuses.
	var old bool
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = $2 AND column_name = 'dirty'
		)`, legacySchema, coreTrackingTable).Scan(&old)
	if err != nil {
		return nil, nil, fmt.Errorf("check the migration table in %s: %w", legacySchema, err)
	}
	if old {
		return nil, target, nil
	}
	return legacy, target, nil
}

func halfMovedError(schema string, legacy, target []relation) error {
	return fmt.Errorf("%w: schema %s holds %s and schema %s holds %s; refusing to start: move the rest by hand, or set aside the copy without data (a controller from before the move creates empty tables in %[2]s)",
		ErrHalfMoved, legacySchema, relationNames(legacy), schema, relationNames(target))
}

// moveVectorExtension moves pgvector from public into the vector source's
// schema when it is installed in public and another schema is configured.
func moveVectorExtension(ctx context.Context, tx *sql.Tx, vector Source) (bool, error) {
	if vector.VectorSchema == "" || vector.VectorSchema == legacySchema {
		return false, nil
	}
	var schema string
	err := tx.QueryRowContext(ctx, `SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = 'vector'`).Scan(&schema)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && schema != legacySchema) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check pgvector schema: %w", err)
	}
	if err := createSchema(ctx, tx, vector.VectorSchema); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, "ALTER EXTENSION vector SET SCHEMA "+quoteIdentifier(vector.VectorSchema)); err != nil {
		return false, fmt.Errorf("move pgvector from %s to %s (its owner may run ALTER EXTENSION vector SET SCHEMA %s, or set the pgvector schema to %s): %w",
			legacySchema, vector.VectorSchema, vector.VectorSchema, legacySchema, err)
	}
	return true, nil
}

func relationExists(ctx context.Context, tx *sql.Tx, schema, name string) (bool, error) {
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", quoteIdentifier(schema)+"."+quoteIdentifier(name)).Scan(&exists); err != nil {
		return false, fmt.Errorf("check %s.%s: %w", schema, name, err)
	}
	return exists, nil
}

func relationOrder(kind string) int {
	switch kind {
	case "r", "p":
		return 0
	case "v", "m":
		return 1
	default:
		return 2
	}
}

func relationKeyword(kind string) string {
	switch kind {
	case "v":
		return "VIEW"
	case "m":
		return "MATERIALIZED VIEW"
	case "S":
		return "SEQUENCE"
	default:
		return "TABLE"
	}
}

func relationNames(rels []relation) string {
	names := make([]string, 0, len(rels))
	for _, rel := range rels {
		names = append(names, rel.name)
	}
	return strings.Join(names, ", ")
}

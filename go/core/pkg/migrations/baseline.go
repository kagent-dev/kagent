package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
)

// runBaseline runs only under the source's advisory lock. The existing history
// remains authoritative once any nonzero ledger entry exists, including after
// a baseline committed but the caller lost its response or a later migration failed.
func runBaseline(ctx context.Context, db *sql.DB, src Source) error {
	table := pgx.Identifier{src.Schema, src.TrackingTable}.Sanitize()
	var exists bool
	err := db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check migration table: %w", err)
	}
	if exists {
		var hasHistory bool
		if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM "+table+" WHERE version_id <> 0)").Scan(&hasHistory); err != nil {
			return fmt.Errorf("read migration history: %w", err)
		}
		if hasHistory {
			return nil
		}
	}

	baseline := src
	baseline.Dir = src.BaselineDir
	cutoffs, err := Versions(baseline)
	if err != nil {
		return err
	}
	if len(cutoffs) != 1 {
		return fmt.Errorf("directory %s must contain exactly one baseline migration", src.BaselineDir)
	}
	versions, err := Versions(src)
	if err != nil {
		return err
	}
	index := slices.Index(versions, cutoffs[0])
	if index < 0 {
		return fmt.Errorf("baseline version %d is not in migration history", cutoffs[0])
	}
	baselineFS, err := fs.Sub(src.FS, src.BaselineDir)
	if err != nil {
		return fmt.Errorf("open baseline directory %s: %w", src.BaselineDir, err)
	}
	store, err := database.NewStore(database.DialectPostgres, src.TrackingTable)
	if err != nil {
		return fmt.Errorf("create baseline migration store: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectCustom, db, baselineFS,
		goose.WithStore(&baselineStore{Store: store, versions: versions[:index+1]}),
		goose.WithDisableGlobalRegistry(true),
		goose.WithLogger(goose.NopLogger()),
	)
	if err != nil {
		return fmt.Errorf("create baseline migration provider: %w", err)
	}
	_, err = provider.Up(ctx)
	return err
}

// baselineStore records the retained history in the baseline's DDL transaction.
// Goose can then apply the tail and use the original Down migrations without
// missing versions. A failed baseline rolls back its schema and every version,
// leaving only Goose's initial version zero for the next startup to retry.
type baselineStore struct {
	database.Store
	versions []int64
}

var _ database.Store = (*baselineStore)(nil)

func (b *baselineStore) Insert(ctx context.Context, db database.DBTxConn, req database.InsertRequest) error {
	if req.Version != b.versions[len(b.versions)-1] {
		return b.Store.Insert(ctx, db, req)
	}
	if _, ok := db.(*sql.Tx); !ok {
		return fmt.Errorf("baseline version %d must run in a transaction", req.Version)
	}
	for _, version := range b.versions {
		if err := b.Store.Insert(ctx, db, database.InsertRequest{Version: version}); err != nil {
			return fmt.Errorf("record baseline version %d: %w", version, err)
		}
	}
	return nil
}

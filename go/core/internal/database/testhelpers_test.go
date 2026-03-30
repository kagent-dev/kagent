package database

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
)

var (
	sharedDB      *pgxpool.Pool
	sharedConnStr string
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}

	connStr, _, err := dbtest.Start(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start postgres container: %v\n", err)
		os.Exit(1)
	}
	sharedConnStr = connStr

	if err := dbtest.Migrate(connStr, true); err != nil {
		fmt.Fprintf(os.Stderr, "failed to migrate test database: %v\n", err)
		os.Exit(1)
	}

	db, err := Connect(context.Background(), &PostgresConfig{URL: connStr, VectorEnabled: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to test database: %v\n", err)
		os.Exit(1)
	}
	sharedDB = db

	os.Exit(m.Run())
}

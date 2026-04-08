package database

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
)

var sharedManager *Manager

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

	sharedManager, err = NewManager(context.Background(), &Config{
		PostgresConfig: &PostgresConfig{
			URL:           connStr,
			VectorEnabled: true,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create shared manager: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

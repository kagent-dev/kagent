package config

import (
	"flag"
	"os"
)

// DBType represents the database backend type.
type DBType string

const (
	DBTypeSQLite   DBType = "sqlite"
	DBTypePostgres DBType = "postgres"
)

// Config holds all runtime settings for the gitrepo-mcp server.
type Config struct {
	Addr      string // --addr / GITREPO_ADDR, default ":8090"
	Transport string // --transport / GITREPO_TRANSPORT, "http" | "stdio"
	DBType    DBType // --db-type / GITREPO_DB_TYPE, "sqlite" | "postgres"
	DBPath    string // --db-path / GITREPO_DB_PATH, default "./data/gitrepo.db"
	DBURL     string // --db-url / GITREPO_DB_URL
	DataDir   string // --data-dir / GITREPO_DATA_DIR, default "./data"
	LogLevel  string // --log-level / GITREPO_LOG_LEVEL, default "info"
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Load parses CLI flags (os.Args[1:]) with GITREPO_* environment variable fallback.
func Load() (*Config, error) {
	return LoadArgs(os.Args[1:])
}

// LoadArgs parses the given args with GITREPO_* environment variable fallback.
func LoadArgs(args []string) (*Config, error) {
	fs := flag.NewFlagSet("gitrepo-mcp", flag.ContinueOnError)

	addr := fs.String("addr", envOrDefault("GITREPO_ADDR", ":8090"), "listen address")
	transport := fs.String("transport", envOrDefault("GITREPO_TRANSPORT", "http"), "transport mode: http or stdio")
	dbType := fs.String("db-type", envOrDefault("GITREPO_DB_TYPE", "sqlite"), "database type: sqlite or postgres")
	dbPath := fs.String("db-path", envOrDefault("GITREPO_DB_PATH", "./data/gitrepo.db"), "SQLite database file path")
	dbURL := fs.String("db-url", envOrDefault("GITREPO_DB_URL", ""), "Postgres connection URL")
	dataDir := fs.String("data-dir", envOrDefault("GITREPO_DATA_DIR", "./data"), "data directory for cloned repos and database")
	logLevel := fs.String("log-level", envOrDefault("GITREPO_LOG_LEVEL", "info"), "log level: debug, info, warn, error")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	return &Config{
		Addr:      *addr,
		Transport: *transport,
		DBType:    DBType(*dbType),
		DBPath:    *dbPath,
		DBURL:     *dbURL,
		DataDir:   *dataDir,
		LogLevel:  *logLevel,
	}, nil
}

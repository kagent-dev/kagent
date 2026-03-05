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

// Config holds all runtime settings for the kanban-mcp server.
type Config struct {
	Addr      string // --addr / KANBAN_ADDR, default ":8080"
	Transport string // --transport / KANBAN_TRANSPORT, "http" | "stdio"
	DBType    DBType // --db-type / KANBAN_DB_TYPE, "sqlite" | "postgres"
	DBPath    string // --db-path / KANBAN_DB_PATH, default "./kanban.db"
	DBURL     string // --db-url / KANBAN_DB_URL
	LogLevel  string // --log-level / KANBAN_LOG_LEVEL, default "info"
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Load parses CLI flags (os.Args[1:]) with KANBAN_* environment variable fallback.
func Load() (*Config, error) {
	return LoadArgs(os.Args[1:])
}

// LoadArgs parses the given args with KANBAN_* environment variable fallback.
// Separated from Load to allow testability without global flag state.
func LoadArgs(args []string) (*Config, error) {
	fs := flag.NewFlagSet("kanban-mcp", flag.ContinueOnError)

	addr := fs.String("addr", envOrDefault("KANBAN_ADDR", ":8080"), "listen address")
	transport := fs.String("transport", envOrDefault("KANBAN_TRANSPORT", "http"), "transport mode: http or stdio")
	dbType := fs.String("db-type", envOrDefault("KANBAN_DB_TYPE", "sqlite"), "database type: sqlite or postgres")
	dbPath := fs.String("db-path", envOrDefault("KANBAN_DB_PATH", "./kanban.db"), "SQLite database file path")
	dbURL := fs.String("db-url", envOrDefault("KANBAN_DB_URL", ""), "Postgres connection URL")
	logLevel := fs.String("log-level", envOrDefault("KANBAN_LOG_LEVEL", "info"), "log level: debug, info, warn, error")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	return &Config{
		Addr:      *addr,
		Transport: *transport,
		DBType:    DBType(*dbType),
		DBPath:    *dbPath,
		DBURL:     *dbURL,
		LogLevel:  *logLevel,
	}, nil
}

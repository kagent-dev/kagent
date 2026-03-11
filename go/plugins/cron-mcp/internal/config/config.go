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

// Config holds all runtime settings for the cron-mcp server.
type Config struct {
	Addr      string // --addr / CRON_ADDR, default ":8080"
	Transport string // --transport / CRON_TRANSPORT, "http" | "stdio"
	DBType    DBType // --db-type / CRON_DB_TYPE, "sqlite" | "postgres"
	DBPath    string // --db-path / CRON_DB_PATH, default "./cron.db"
	DBURL     string // --db-url / CRON_DB_URL
	LogLevel  string // --log-level / CRON_LOG_LEVEL, default "info"
	Shell     string // --shell / CRON_SHELL, default "/bin/sh"
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Load parses CLI flags (os.Args[1:]) with CRON_* environment variable fallback.
func Load() (*Config, error) {
	return LoadArgs(os.Args[1:])
}

// LoadArgs parses the given args with CRON_* environment variable fallback.
func LoadArgs(args []string) (*Config, error) {
	fs := flag.NewFlagSet("cron-mcp", flag.ContinueOnError)

	addr := fs.String("addr", envOrDefault("CRON_ADDR", ":8080"), "listen address")
	transport := fs.String("transport", envOrDefault("CRON_TRANSPORT", "http"), "transport mode: http or stdio")
	dbType := fs.String("db-type", envOrDefault("CRON_DB_TYPE", "sqlite"), "database type: sqlite or postgres")
	dbPath := fs.String("db-path", envOrDefault("CRON_DB_PATH", "./cron.db"), "SQLite database file path")
	dbURL := fs.String("db-url", envOrDefault("CRON_DB_URL", ""), "Postgres connection URL")
	logLevel := fs.String("log-level", envOrDefault("CRON_LOG_LEVEL", "info"), "log level: debug, info, warn, error")
	shell := fs.String("shell", envOrDefault("CRON_SHELL", "/bin/sh"), "shell to execute commands")

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
		Shell:     *shell,
	}, nil
}

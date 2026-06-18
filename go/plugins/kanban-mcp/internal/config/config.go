// Package config loads kanban-mcp server configuration from command-line flags
// with environment-variable fallback. Postgres is the only supported database;
// there is no SQLite or --db-type switch.
package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
)

// Default configuration values.
const (
	defaultAddr      = ":8080"
	defaultTransport = "stdio"
	defaultLogLevel  = "info"
)

// Environment variable names mirrored by the command-line flags.
const (
	envAddr       = "KANBAN_ADDR"
	envTransport  = "KANBAN_TRANSPORT"
	envDBURL      = "KANBAN_DB_URL"
	envDBURLFile  = "KANBAN_DB_URL_FILE"
	envLogLevel   = "KANBAN_LOG_LEVEL"
	envBoards     = "KANBAN_BOARDS"
	envBoardsFile = "KANBAN_BOARDS_FILE"
	envReadonly   = "KANBAN_READONLY"
)

// Config holds the resolved runtime configuration for the kanban-mcp server.
type Config struct {
	Addr       string // --addr / KANBAN_ADDR, default ":8080"
	Transport  string // --transport / KANBAN_TRANSPORT, "http" | "stdio", default "stdio"
	DBURL      string // --db-url / KANBAN_DB_URL (Postgres connection URL)
	DBURLFile  string // --db-url-file / KANBAN_DB_URL_FILE (file containing URL; takes precedence)
	LogLevel   string // --log-level / KANBAN_LOG_LEVEL, default "info"
	Boards     string // --boards / KANBAN_BOARDS (inline JSON board definitions to seed)
	BoardsFile string // --boards-file / KANBAN_BOARDS_FILE (path to a JSON file of board definitions; takes precedence)
	Readonly   bool   // --readonly / KANBAN_READONLY, default false; serves the board UI read-only (hides the "New Task" button)
}

// Load parses command-line flags (from os.Args[1:]) and falls back to the
// corresponding environment variables for any flag the user did not set.
func Load() (*Config, error) {
	return loadArgs(os.Args[0], os.Args[1:])
}

// loadArgs is the testable core of Load: it parses the given args using a fresh
// FlagSet so tests can exercise it without touching the global flag state.
func loadArgs(prog string, args []string) (*Config, error) {
	fs := flag.NewFlagSet(prog, flag.ContinueOnError)

	addr := fs.String("addr", defaultAddr, "listen address for the HTTP transport (env "+envAddr+")")
	transport := fs.String("transport", defaultTransport, `transport: "http" or "stdio" (env `+envTransport+")")
	dbURL := fs.String("db-url", "", "Postgres connection URL (env "+envDBURL+")")
	dbURLFile := fs.String("db-url-file", "", "file containing the Postgres connection URL; takes precedence over --db-url (env "+envDBURLFile+")")
	logLevel := fs.String("log-level", defaultLogLevel, "log level (env "+envLogLevel+")")
	boards := fs.String("boards", "", "inline JSON array of board definitions to seed on startup (env "+envBoards+")")
	boardsFile := fs.String("boards-file", "", "path to a JSON file of board definitions to seed; takes precedence over --boards (env "+envBoardsFile+")")
	readonly := fs.Bool("readonly", false, "serve the board UI in read-only mode, hiding the \"New Task\" button (env "+envReadonly+")")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("parsing flags: %w", err)
	}

	// Track which flags were explicitly set so env vars only fill the gaps.
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	cfg := &Config{
		Addr:       resolve(set, "addr", *addr, envAddr, defaultAddr),
		Transport:  resolve(set, "transport", *transport, envTransport, defaultTransport),
		DBURL:      resolve(set, "db-url", *dbURL, envDBURL, ""),
		DBURLFile:  resolve(set, "db-url-file", *dbURLFile, envDBURLFile, ""),
		LogLevel:   resolve(set, "log-level", *logLevel, envLogLevel, defaultLogLevel),
		Boards:     resolve(set, "boards", *boards, envBoards, ""),
		BoardsFile: resolve(set, "boards-file", *boardsFile, envBoardsFile, ""),
		Readonly:   resolveBool(set, "readonly", *readonly, envReadonly),
	}

	return cfg, nil
}

// resolveBool is the boolean counterpart of resolve: an explicitly-set flag
// wins, otherwise a parseable environment variable, otherwise the flag's
// default value.
func resolveBool(set map[string]bool, flagName string, flagValue bool, envKey string) bool {
	if set[flagName] {
		return flagValue
	}
	if v := os.Getenv(envKey); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return flagValue
}

// resolve picks the configuration value for a single field: an explicitly-set
// flag wins, otherwise a non-empty environment variable, otherwise the default.
func resolve(set map[string]bool, flagName, flagValue, envKey, def string) string {
	if set[flagName] {
		return flagValue
	}
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if flagValue != "" {
		return flagValue
	}
	return def
}

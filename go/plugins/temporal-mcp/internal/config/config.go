package config

import (
	"flag"
	"os"
	"time"
)

// Config holds all runtime settings for the temporal-mcp server.
type Config struct {
	Addr              string        // --addr / TEMPORAL_ADDR, default ":8080"
	Transport         string        // --transport / TEMPORAL_TRANSPORT, "http" | "stdio"
	TemporalHostPort  string        // --temporal-host-port / TEMPORAL_HOST_PORT
	TemporalNamespace string        // --temporal-namespace / TEMPORAL_NAMESPACE
	PollInterval      time.Duration // --poll-interval / TEMPORAL_POLL_INTERVAL
	LogLevel          string        // --log-level / TEMPORAL_LOG_LEVEL
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Load parses CLI flags (os.Args[1:]) with TEMPORAL_* environment variable fallback.
func Load() (*Config, error) {
	return LoadArgs(os.Args[1:])
}

// LoadArgs parses the given args with TEMPORAL_* environment variable fallback.
func LoadArgs(args []string) (*Config, error) {
	fs := flag.NewFlagSet("temporal-mcp", flag.ContinueOnError)

	addr := fs.String("addr", envOrDefault("TEMPORAL_ADDR", ":8080"), "listen address")
	transport := fs.String("transport", envOrDefault("TEMPORAL_TRANSPORT", "http"), "transport mode: http or stdio")
	hostPort := fs.String("temporal-host-port", envOrDefault("TEMPORAL_HOST_PORT", "temporal-server:7233"), "Temporal gRPC address")
	namespace := fs.String("temporal-namespace", envOrDefault("TEMPORAL_NAMESPACE", "kagent"), "Temporal namespace")
	pollIntervalStr := fs.String("poll-interval", envOrDefault("TEMPORAL_POLL_INTERVAL", "5s"), "SSE poll interval")
	logLevel := fs.String("log-level", envOrDefault("TEMPORAL_LOG_LEVEL", "info"), "log level: debug, info, warn, error")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	pollInterval, err := time.ParseDuration(*pollIntervalStr)
	if err != nil {
		pollInterval = 5 * time.Second
	}

	return &Config{
		Addr:              *addr,
		Transport:         *transport,
		TemporalHostPort:  *hostPort,
		TemporalNamespace: *namespace,
		PollInterval:      pollInterval,
		LogLevel:          *logLevel,
	}, nil
}

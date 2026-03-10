package config

import (
	"flag"
	"os"
	"strconv"
)

// Config holds all configuration for the nats-activity-feed server.
type Config struct {
	NATSAddr   string
	Addr       string
	BufferSize int
	Subject    string
}

// Load parses config from os.Args[1:].
func Load() (*Config, error) {
	return LoadArgs(os.Args[1:])
}

// LoadArgs parses config from the given args slice (for testability).
func LoadArgs(args []string) (*Config, error) {
	fs := flag.NewFlagSet("nats-activity-feed", flag.ContinueOnError)

	natsAddr := fs.String("nats-addr", envOrDefault("NATS_ADDR", "nats://localhost:4222"), "NATS server address")
	addr := fs.String("addr", envOrDefault("ACTIVITY_FEED_ADDR", ":8090"), "HTTP listen address")
	bufferSize := fs.Int("buffer-size", envOrDefaultInt("ACTIVITY_FEED_BUFFER", 100), "Ring buffer size for new subscribers")
	subject := fs.String("subject", envOrDefault("ACTIVITY_FEED_SUBJECT", "agent.>"), "NATS subject pattern to subscribe to")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	return &Config{
		NATSAddr:   *natsAddr,
		Addr:       *addr,
		BufferSize: *bufferSize,
		Subject:    *subject,
	}, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrDefaultInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

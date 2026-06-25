// Command acp-shim exposes a stdio ACP agent over a WebSocket endpoint.
//
// It is the in-sandbox half of kagent's ACP integration: Substrate's only
// ingress is the network, so this shim listens for a single WebSocket client
// (the kagent A2A↔ACP bridge), spawns the configured agent subprocess, and
// pumps frames — one WebSocket text frame per newline-delimited JSON-RPC
// line — without ever parsing the protocol. The child command after "--" is
// the only per-backend configuration.
//
// Usage:
//
//	acp-shim --listen :9000 -- hermes acp
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/acpshim"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg := &acpshim.Config{}
	flag.StringVar(&cfg.ListenAddr, "listen", ":9000", "address to serve the WebSocket endpoint on")
	flag.StringVar(&cfg.ChildDir, "workdir", "", "working directory for the agent process")
	flag.DurationVar(&cfg.GracePeriod, "grace", 5*time.Second, "SIGTERM-to-SIGKILL grace period when stopping the agent")
	flag.DurationVar(&cfg.ReconnectGrace, "reconnect-grace", 0, "how long the agent survives after the client disconnects (0 = forever)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] -- <agent command> [args...]\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	cfg.ChildArgv = flag.Args()
	acpshim.LoadConfig(cfg)

	if err := cfg.Validate(); err != nil {
		log.Fatalf("acp-shim: %v", err)
	}

	srv := acpshim.NewServer(cfg)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("acp-shim: received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), cfg.GracePeriod+5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("acp-shim: shutdown error: %v", err)
		}
	}()

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("acp-shim: %v", err)
	}
}

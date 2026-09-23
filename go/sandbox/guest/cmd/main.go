// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agent-substrate/env/guest"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

func main() {
	cfg := guest.Config{EnableProcess: true, EnableFileSystem: true}
	flag.StringVar(&cfg.ListenAddr, "listen", ":80", "address to serve the guest API on")
	flag.StringVar(&cfg.Workspace, "workspace", "/data/workspace", "default process working directory and file API root")
	flag.StringVar(&cfg.LogDir, "log-dir", "/data/guest-logs", "directory for process output logs")
	flag.Parse()

	logger, err := logging.NewFromEnv(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(logging.IntoContext(context.Background(), logger), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		logger.ErrorContext(ctx, "sandbox guest stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg guest.Config) error {
	grpcServer, cleanup, err := guest.NewServer(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize guest services: %w", err)
	}
	defer cleanup()
	// End output observers before upstream cleanup waits for RPCs to finish.
	defer grpcServer.Stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("ok\n")); err != nil {
			logging.FromContext(ctx).DebugContext(r.Context(), "failed to write readiness response", "error", err)
		}
	})
	mux.Handle("/", grpcServer)

	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		Protocols:         &protocols,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.ListenAndServe() }()
	logging.FromContext(ctx).InfoContext(ctx, "starting sandbox guest", "listen_address", cfg.ListenAddr, "workspace", cfg.Workspace, "log_dir", cfg.LogDir)

	select {
	case err = <-serveErrors:
	case <-ctx.Done():
		logging.FromContext(ctx).InfoContext(ctx, "stopping sandbox guest")
		grpcServer.Stop()
		if err := server.Close(); err != nil {
			return fmt.Errorf("failed to close guest listener: %w", err)
		}
		err = <-serveErrors
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to serve guest API: %w", err)
	}
	return nil
}

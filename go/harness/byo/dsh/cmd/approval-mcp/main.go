// approval-mcp runs independently of the DSH Actor and its process lifetime.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/operations"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

func main() {
	path := flag.String("config", "", "Path to deployment JSON configuration")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx = logging.IntoContext(ctx, slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(ctx, *path); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "approval service stopped", "error", err)
		os.Exit(1)
	}
}

func readSecret(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read secret file: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", errors.New("secret file is empty")
	}
	return value, nil
}

func run(ctx context.Context, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open config: %w", err)
	}
	defer file.Close()
	var config operations.Config
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return fmt.Errorf("failed to decode config: %w", err)
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return errors.New("trailing configuration data")
	}
	if err := config.Validate(); err != nil {
		return err
	}
	if config.Listen == "" || config.DatabaseURLFile == "" || config.AgentTokenFile == "" || config.ApproverTokenFile == "" {
		return errors.New("listen and database/agent/approver secret file paths are required")
	}
	dsn, err := readSecret(config.DatabaseURLFile)
	if err != nil {
		return err
	}
	agent, err := readSecret(config.AgentTokenFile)
	if err != nil {
		return err
	}
	approver, err := readSecret(config.ApproverTokenFile)
	if err != nil {
		return err
	}
	upstream, err := readSecret(config.UpstreamTokenFile)
	if err != nil {
		return err
	}
	store, err := operations.OpenStore(ctx, dsn, config.Scope)
	if err != nil {
		return err
	}
	defer store.Close()
	var notifier operations.Notifier
	if config.Continuation != nil {
		token, err := readSecret(config.Continuation.TokenFile)
		if err != nil {
			return err
		}
		notifier = operations.NewA2ANotifier(*config.Continuation, token)
	}
	service, err := operations.New(store, config, operations.NewMCPCaller(config.UpstreamURL, upstream, time.Duration(config.CallTimeoutSeconds)*time.Second), notifier)
	if err != nil {
		return err
	}
	handler, err := service.Handler(agent, approver)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: config.Listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return service.Run(ctx) })
	group.Go(func() error {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	})
	group.Go(func() error {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	})
	return group.Wait()
}

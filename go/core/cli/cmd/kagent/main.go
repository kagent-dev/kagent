package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kagent-dev/kagent/go/core/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := cli.Root().ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		var processError interface{ ExitCode() int }
		if errors.As(err, &processError) {
			os.Exit(processError.ExitCode())
		}
		os.Exit(1)
	}
}

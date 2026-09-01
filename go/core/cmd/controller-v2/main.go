package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/kagent-dev/kagent/go/core/pkg/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// No options: core's own controller runs with the default authenticator and
	// authorizer. A library consumer supplies its own by calling app.Run directly.
	if err := app.Run(ctx, app.Options{}); err != nil {
		log.Fatal(err)
	}
}

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/app"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
)

func main() {
	if err := app.SetupLogger(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logger := slog.Default()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	switch os.Getenv("KAGENT_DATABASE_BOOTSTRAP") {
	case "", "false":
	case "true":
		if err := runDatabaseBootstrap(ctx); err != nil {
			logger.ErrorContext(ctx, "database bootstrap failed", "error", err)
			os.Exit(1)
		}
	default:
		logger.ErrorContext(ctx, "invalid database bootstrap value")
		os.Exit(1)
	}

	// No options: core's own controller runs with the default authenticator and
	// authorizer. A library consumer supplies its own by calling app.Run directly.
	if err := app.Run(ctx, app.Options{}); err != nil {
		logger.ErrorContext(ctx, "controller stopped", "error", err)
		os.Exit(1)
	}
}

func runDatabaseBootstrap(ctx context.Context) error {
	adminUsername, err := readRequiredFile("POSTGRES_ADMIN_USERNAME_FILE")
	if err != nil {
		return err
	}
	adminPassword, err := readRequiredFile("POSTGRES_ADMIN_PASSWORD_FILE")
	if err != nil {
		return err
	}
	return database.Bootstrap(ctx, database.BootstrapConfig{
		EndpointSource: os.Getenv("POSTGRES_DATABASE_URL"),
		AdminUsername:  adminUsername,
		AdminPassword:  adminPassword,
		Schema:         kagentenv.DatabaseSchema.Get(),
		VectorEnabled:  kagentenv.DatabaseVectorEnabled.Get(),
		VectorSchema:   kagentenv.DatabaseVectorSchema.Get(),
	})
}

func readRequiredFile(envName string) (string, error) {
	path := os.Getenv(envName)
	if path == "" {
		return "", fmt.Errorf("%s must name a credential file", envName)
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", envName, err)
	}
	if value := strings.TrimSpace(string(value)); value != "" {
		return value, nil
	}
	return "", fmt.Errorf("%s credential file is empty", envName)
}

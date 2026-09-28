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
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/app"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

func main() {
	if err := app.SetupLogger(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logger := slog.Default()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	switch env.DatabaseBootstrap.Get() {
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

	authenticator, err := controllerAuthenticator(env.AuthMode.Get(), env.AuthUserIDClaim.Get())
	if err != nil {
		logger.ErrorContext(ctx, "invalid controller authentication configuration", "error", err)
		os.Exit(1)
	}
	if err := app.Run(ctx, app.Options{Authenticator: authenticator}); err != nil {
		logger.ErrorContext(ctx, "controller stopped", "error", err)
		os.Exit(1)
	}
}

func runDatabaseBootstrap(ctx context.Context) error {
	adminUsername, err := readRequiredFile(env.PostgresAdminUsernameFile)
	if err != nil {
		return err
	}
	adminPassword, err := readRequiredFile(env.PostgresAdminPasswordFile)
	if err != nil {
		return err
	}
	return database.Bootstrap(ctx, database.BootstrapConfig{
		EndpointSource: env.PostgresDatabaseURL.Get(),
		AdminUsername:  adminUsername,
		AdminPassword:  adminPassword,
		Schema:         env.DatabaseSchema.Get(),
		VectorEnabled:  env.DatabaseVectorEnabled.Get(),
		VectorSchema:   env.DatabaseVectorSchema.Get(),
	})
}

func readRequiredFile(variable env.StringVar) (string, error) {
	envName := variable.Name()
	path := variable.Get()
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

func controllerAuthenticator(mode, userIDClaim string) (auth.AuthProvider, error) {
	switch mode {
	case env.AuthModeInsecure:
		return &authimpl.InsecureAuthenticator{}, nil
	case env.AuthModeTrustedProxy:
		return authimpl.NewProxyAuthenticator(userIDClaim), nil
	default:
		return nil, fmt.Errorf("unsupported %s %q: expected %s or %s", env.AuthMode.Name(), mode, env.AuthModeInsecure, env.AuthModeTrustedProxy)
	}
}

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

/*
Copyright 2025.

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
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/pkg/app"
	pkgauth "github.com/kagent-dev/kagent/go/pkg/auth"
)

func main() {
	authenticator := &auth.UnsecureAuthenticator{}
	app.Start(func(bootstrap app.BootstrapConfig) (*app.ExtensionConfig, error) {
		var authorizer pkgauth.Authorizer
		if endpoint := os.Getenv("EXTERNAL_AUTHZ_ENDPOINT"); endpoint != "" {
			provider, err := auth.ProviderByName(os.Getenv("AUTHZ_PROVIDER"))
			if err != nil {
				return nil, fmt.Errorf("invalid authz provider: %w", err)
			}
			authorizer = &auth.ExternalAuthorizer{
				Endpoint: endpoint,
				Provider: provider,
				Client:   &http.Client{Timeout: 5 * time.Second},
			}
		} else {
			authorizer = &auth.NoopAuthorizer{}
		}

		return &app.ExtensionConfig{
			Authenticator: authenticator,
			Authorizer:    authorizer,
		}, nil
	})
}

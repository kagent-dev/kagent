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
	"os"

	"github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/pkg/app"
	pkgauth "github.com/kagent-dev/kagent/go/pkg/auth"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

// nolint:gocyclo
func main() {
	authorizer := &auth.NoopAuthorizer{}
	authenticator := getAuthenticator()
	app.Start(func(bootstrap app.BootstrapConfig) (*app.ExtensionConfig, error) {
		return &app.ExtensionConfig{
			Authenticator:    authenticator,
			Authorizer:       authorizer,
			AgentPlugins:     nil,
			MCPServerPlugins: nil,
		}, nil
	})
}

func getAuthenticator() pkgauth.AuthProvider {
	switch os.Getenv("AUTH_MODE") {
	case "proxy":
		claimsConfig := auth.ClaimsConfig{
			UserID: os.Getenv("AUTH_JWT_CLAIM_USER_ID"),
			Email:  os.Getenv("AUTH_JWT_CLAIM_EMAIL"),
			Name:   os.Getenv("AUTH_JWT_CLAIM_NAME"),
			Groups: os.Getenv("AUTH_JWT_CLAIM_GROUPS"),
		}
		return auth.NewProxyAuthenticator(claimsConfig)
	default:
		return &auth.UnsecureAuthenticator{}
	}
}

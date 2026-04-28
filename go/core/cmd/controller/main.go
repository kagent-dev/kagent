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
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/app"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/agentsxk8s"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

//nolint:gocyclo
func main() {
	app.Start(func(bootstrap app.BootstrapConfig) (*app.ExtensionConfig, error) {
		authenticator := getAuthenticator(bootstrap.Config.Auth)
		authorizer := getAuthorizer(bootstrap.Config.Auth)
		return &app.ExtensionConfig{
			Authenticator:  authenticator,
			Authorizer:     authorizer,
			AgentPlugins:   nil,
			SandboxBackend: agentsxk8s.New(),
		}, nil
	}, nil)
}

func getAuthenticator(authCfg struct{ Mode, UserIDClaim string }) pkgauth.AuthProvider {
	switch authCfg.Mode {
	case "trusted-proxy":
		return auth.NewProxyAuthenticator(authCfg.UserIDClaim)
	case "unsecure":
		return &auth.UnsecureAuthenticator{}
	default:
		panic("unknown auth mode: " + authCfg.Mode + " (valid modes: unsecure, trusted-proxy)")
	}
}

func getAuthorizer(authCfg struct{ Mode, UserIDClaim string }) pkgauth.Authorizer {
	switch authCfg.Mode {
	case "trusted-proxy":
		// GroupAuthorizer with nil client — app.go will inject the kube client later
		return auth.NewGroupAuthorizer(nil)
	default:
		return &auth.NoopAuthorizer{}
	}
}

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
	"syscall"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/app"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
	if err := app.SetupLogger(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logger := slog.Default()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var tokenReviews authimpl.TokenReviewer
	if env.AuthorizationMode.Get() == env.AuthorizationModeKubernetes {
		clientset, err := kubernetesClientset()
		if err != nil {
			logger.ErrorContext(ctx, "invalid controller authentication configuration", "error", err)
			os.Exit(1)
		}
		tokenReviews = clientset.AuthenticationV1().TokenReviews()
	}
	authenticator, err := controllerAuthenticator(env.AuthMode.Get(), env.AuthUserIDClaim.Get(), env.AuthorizationGroupsClaim.Get(),
		tokenReviews, env.AuthorizationServiceAccountTokenAudience.Get())
	if err != nil {
		logger.ErrorContext(ctx, "invalid controller authentication configuration", "error", err)
		os.Exit(1)
	}
	authorizer, err := controllerAuthorizer(env.AuthorizationMode.Get(), authorizationConfigFromEnv())
	if err != nil {
		logger.ErrorContext(ctx, "invalid controller authorization configuration", "error", err)
		os.Exit(1)
	}
	if env.AuthorizationMode.Get() == env.AuthorizationModeKubernetes && env.AuthMode.Get() == env.AuthModeInsecure {
		// The caller names its own user ID, so it picks whose RBAC applies.
		logger.WarnContext(ctx, "kubernetes authorization with insecure authentication protects nothing: any caller can name the user it is reviewed as",
			"authentication", env.AuthMode.Name(), "authorization", env.AuthorizationMode.Name())
	}
	if err := app.Run(ctx, app.Options{Authenticator: authenticator, Authorizer: authorizer}); err != nil {
		logger.ErrorContext(ctx, "controller stopped", "error", err)
		os.Exit(1)
	}
}

// controllerAuthenticator builds the authenticator of mode. Under Kubernetes
// authorization, which passes the API server's tokenReviews, a ServiceAccount
// token is authenticated with a TokenReview for serviceAccountAudience, and a
// person's token without the user ID claim is refused rather than read by its
// sub.
func controllerAuthenticator(mode, userIDClaim, groupsClaim string, tokenReviews authimpl.TokenReviewer, serviceAccountAudience string) (auth.AuthProvider, error) {
	switch mode {
	case env.AuthModeInsecure:
		return &authimpl.InsecureAuthenticator{}, nil
	case env.AuthModeTrustedProxy:
		options := []authimpl.ProxyAuthenticatorOption{authimpl.WithGroupsClaim(groupsClaim)}
		if tokenReviews != nil {
			if serviceAccountAudience == "" {
				return nil, fmt.Errorf("%s must not be empty in %s %s", env.AuthorizationServiceAccountTokenAudience.Name(), env.AuthorizationMode.Name(), env.AuthorizationModeKubernetes)
			}
			options = append(options, authimpl.WithUserIDClaimRequired(), authimpl.WithServiceAccountTokenReview(tokenReviews, serviceAccountAudience))
		}
		return authimpl.NewProxyAuthenticator(userIDClaim, options...), nil
	default:
		return nil, fmt.Errorf("unsupported %s %q: expected %s or %s", env.AuthMode.Name(), mode, env.AuthModeInsecure, env.AuthModeTrustedProxy)
	}
}

// authorizationConfig is the KubernetesAuthorizer's settings as the
// environment spells them.
type authorizationConfig struct {
	usernamePrefix, groupsPrefix, namespaceSelector, includeNamespaces, watchNamespaces string
}

func authorizationConfigFromEnv() authorizationConfig {
	return authorizationConfig{
		usernamePrefix: env.AuthorizationUsernamePrefix.Get(), groupsPrefix: env.AuthorizationGroupsPrefix.Get(),
		namespaceSelector: env.AuthorizationNamespaceSelector.Get(), includeNamespaces: env.AuthorizationIncludeNamespaces.Get(),
		watchNamespaces: env.WatchNamespaces.Get(),
	}
}

// controllerAuthorizer selects the authorizer. The kubernetes mode asks the
// API server, so it needs the controller's own Kubernetes client; the noop
// mode needs nothing and keeps a cluster it cannot reach out of startup.
func controllerAuthorizer(mode string, settings authorizationConfig) (auth.CollectionAuthorizer, error) {
	switch mode {
	case env.AuthorizationModeNoop:
		return &auth.NoopAuthorizer{}, nil
	case env.AuthorizationModeKubernetes:
		selector, err := labels.Parse(settings.namespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid %s %q: %w", env.AuthorizationNamespaceSelector.Name(), settings.namespaceSelector, err)
		}
		clientset, err := kubernetesClientset()
		if err != nil {
			return nil, err
		}
		authorizer, err := auth.NewKubernetesAuthorizer(clientset.AuthorizationV1().SubjectAccessReviews(), clientset.CoreV1().Namespaces(), auth.KubernetesConfig{
			UsernamePrefix: settings.usernamePrefix, GroupsPrefix: settings.groupsPrefix,
			Namespaces: app.WatchNamespaces(settings.watchNamespaces), NamespaceSelector: selector,
			IncludedNamespaces: app.WatchNamespaces(settings.includeNamespaces),
		})
		if err != nil {
			return nil, fmt.Errorf("configure %s %s: %w", env.AuthorizationMode.Name(), mode, err)
		}
		return authorizer, nil
	default:
		return nil, fmt.Errorf("unsupported %s %q: expected %s or %s", env.AuthorizationMode.Name(), mode, env.AuthorizationModeNoop, env.AuthorizationModeKubernetes)
	}
}

// kubernetesClientset is the controller's own client of the API server, which
// the kubernetes authorization mode reviews tokens and callers with.
func kubernetesClientset() (*kubernetes.Clientset, error) {
	restConfig, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return clientset, nil
}

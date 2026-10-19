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
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/app"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
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

	// The kubernetes mode reviews tokens and callers with the controller's own
	// client; the noop mode needs none and keeps a cluster it cannot reach out
	// of startup.
	var clientset kubernetes.Interface
	var tokenReviews authimpl.TokenReviewer
	if env.AuthorizationMode.Get() == env.AuthorizationModeKubernetes {
		var err error
		clientset, err = kubernetesClientset()
		if err != nil {
			logger.ErrorContext(ctx, "invalid controller authorization configuration", "error", err)
			os.Exit(1)
		}
		tokenReviews = clientset.AuthenticationV1().TokenReviews()
	}
	authenticator, err := controllerAuthenticator(env.AuthMode.Get(), env.AuthUserIDClaim.Get(), env.AuthorizationGroupsClaim.Get(),
		tokenReviews, env.AuthorizationServiceAccountTokenAudience.Get(), env.AuthorizationSessionReaderServiceAccount.Get())
	if err != nil {
		logger.ErrorContext(ctx, "invalid controller authentication configuration", "error", err)
		os.Exit(1)
	}
	authorizer, err := controllerAuthorizer(env.AuthorizationMode.Get(), authorizationConfigFromEnv(), clientset)
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
// sub. sessionReader (<namespace>/<name>) names the ServiceAccount admitted as
// the session reader; it needs the TokenReview, so it is refused without it.
func controllerAuthenticator(mode, userIDClaim, groupsClaim string, tokenReviews authimpl.TokenReviewer, serviceAccountAudience, sessionReader string) (auth.AuthProvider, error) {
	if sessionReader != "" && (mode != env.AuthModeTrustedProxy || tokenReviews == nil) {
		return nil, fmt.Errorf("%s needs %s %s and %s %s", env.AuthorizationSessionReaderServiceAccount.Name(),
			env.AuthMode.Name(), env.AuthModeTrustedProxy, env.AuthorizationMode.Name(), env.AuthorizationModeKubernetes)
	}
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
		if sessionReader != "" {
			user, err := serviceAccountUser(sessionReader)
			if err != nil {
				return nil, fmt.Errorf("invalid %s %q: %w", env.AuthorizationSessionReaderServiceAccount.Name(), sessionReader, err)
			}
			options = append(options, authimpl.WithSessionReader(user))
		}
		return authimpl.NewProxyAuthenticator(userIDClaim, options...), nil
	default:
		return nil, fmt.Errorf("unsupported %s %q: expected %s or %s", env.AuthMode.Name(), mode, env.AuthModeInsecure, env.AuthModeTrustedProxy)
	}
}

// serviceAccountUser is the user name the API server authenticates the
// ServiceAccount <namespace>/<name> as.
func serviceAccountUser(namespacedName string) (string, error) {
	namespace, name, ok := strings.Cut(namespacedName, "/")
	if !ok {
		return "", errors.New("expected <namespace>/<name>")
	}
	if problems := validation.IsDNS1123Label(namespace); len(problems) > 0 {
		return "", fmt.Errorf("namespace %q: %s", namespace, strings.Join(problems, "; "))
	}
	if problems := validation.IsDNS1123Subdomain(name); len(problems) > 0 {
		return "", fmt.Errorf("name %q: %s", name, strings.Join(problems, "; "))
	}
	return "system:serviceaccount:" + namespace + ":" + name, nil
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
// API server through clientset, the controller's own client; the noop mode
// needs none.
func controllerAuthorizer(mode string, settings authorizationConfig, clientset kubernetes.Interface) (auth.CollectionAuthorizer, error) {
	switch mode {
	case env.AuthorizationModeNoop:
		return &auth.NoopAuthorizer{}, nil
	case env.AuthorizationModeKubernetes:
		selector, err := labels.Parse(settings.namespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid %s %q: %w", env.AuthorizationNamespaceSelector.Name(), settings.namespaceSelector, err)
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

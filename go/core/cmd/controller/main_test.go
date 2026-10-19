package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestControllerAuthenticator(t *testing.T) {
	for _, tt := range []struct {
		name, mode, claim, payload, wantUser string
		wantConfigError, wantAuthError       bool
	}{
		{name: "default", mode: env.AuthMode.DefaultValue(), wantUser: "admin@kagent.dev"},
		{name: "subject", mode: env.AuthModeTrustedProxy, payload: `{"sub":"subject","email":"user@example.com"}`, wantUser: "subject"},
		{name: "email", mode: env.AuthModeTrustedProxy, claim: "email", payload: `{"sub":"subject","email":"user@example.com"}`, wantUser: "user@example.com"},
		{name: "fallback", mode: env.AuthModeTrustedProxy, claim: "email", payload: `{"sub":"subject"}`, wantUser: "subject"},
		{name: "empty custom claim", mode: env.AuthModeTrustedProxy, claim: "email", payload: `{"sub":"subject","email":""}`, wantUser: "subject"},
		{name: "non-string custom claim", mode: env.AuthModeTrustedProxy, claim: "email", payload: `{"sub":"subject","email":42}`, wantUser: "subject"},
		{name: "no identity", mode: env.AuthModeTrustedProxy, claim: "email", payload: `{}`, wantAuthError: true},
		{name: "non-string subject", mode: env.AuthModeTrustedProxy, payload: `{"sub":42}`, wantAuthError: true},
		{name: "missing credentials", mode: env.AuthModeTrustedProxy, wantAuthError: true},
		{name: "malformed credentials", mode: env.AuthModeTrustedProxy, payload: "invalid", wantAuthError: true},
		{name: "unsupported mode", mode: "oidc", wantConfigError: true},
		{name: "empty mode", mode: "", wantConfigError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			authenticator, err := controllerAuthenticator(tt.mode, tt.claim, "", nil, "", "")
			if tt.wantConfigError {
				require.ErrorContains(t, err, env.AuthMode.Name())
				require.Nil(t, authenticator)
				return
			}
			require.NoError(t, err)
			headers := http.Header{}
			if tt.payload != "" {
				headers.Set("Authorization", "Bearer e30."+base64.RawURLEncoding.EncodeToString([]byte(tt.payload))+".signature")
			}
			session, err := authenticator.Authenticate(context.Background(), headers, url.Values{})
			if tt.wantAuthError {
				require.ErrorIs(t, err, authimpl.ErrUnauthenticated)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantUser, session.Principal().User.ID)
		})
	}
}

// fakeTokenReviewer authenticates every token as one ServiceAccount.
type fakeTokenReviewer struct{}

func (fakeTokenReviewer) Create(_ context.Context, review *authenticationv1.TokenReview, _ metav1.CreateOptions) (*authenticationv1.TokenReview, error) {
	result := review.DeepCopy()
	result.Status = authenticationv1.TokenReviewStatus{Authenticated: true, Audiences: review.Spec.Audiences, User: authenticationv1.UserInfo{Username: "system:serviceaccount:workspace-manager:workspace-manager"}}
	return result, nil
}

func TestControllerAuthenticatorServiceAccountToken(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer e30."+base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"system:serviceaccount:workspace-manager:workspace-manager"}`))+".signature")
	for _, tt := range []struct {
		name    string
		reviews authimpl.TokenReviewer
	}{
		// noop authorization passes no TokenReview client: the token's sub
		// names the user, as before.
		{name: "noop authorization reads the sub"},
		// Only the TokenReview marks the user as the API server's own.
		{name: "kubernetes authorization reviews the token", reviews: fakeTokenReviewer{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			authenticator, err := controllerAuthenticator(env.AuthModeTrustedProxy, "email", "", tt.reviews, "kagent", "")
			require.NoError(t, err)
			session, err := authenticator.Authenticate(context.Background(), headers, url.Values{})
			require.NoError(t, err)
			require.Equal(t, "system:serviceaccount:workspace-manager:workspace-manager", session.Principal().User.ID)
			require.Equal(t, tt.reviews != nil, session.Principal().User.Kubernetes)
		})
	}
}

func TestControllerAuthenticatorSessionReader(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer e30."+base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"system:serviceaccount:workspace-manager:workspace-manager"}`))+".signature")
	for _, tt := range []struct {
		name, mode, reader string
		reviews            authimpl.TokenReviewer
		wantConfigError    string
	}{
		{name: "the configured ServiceAccount is the session reader", mode: env.AuthModeTrustedProxy, reader: "workspace-manager/workspace-manager", reviews: fakeTokenReviewer{}},
		{name: "no namespace", mode: env.AuthModeTrustedProxy, reader: "workspace-manager", reviews: fakeTokenReviewer{}, wantConfigError: env.AuthorizationSessionReaderServiceAccount.Name()},
		{name: "an invalid namespace", mode: env.AuthModeTrustedProxy, reader: "Workspace_Manager/workspace-manager", reviews: fakeTokenReviewer{}, wantConfigError: env.AuthorizationSessionReaderServiceAccount.Name()},
		{name: "an empty name", mode: env.AuthModeTrustedProxy, reader: "workspace-manager/", reviews: fakeTokenReviewer{}, wantConfigError: env.AuthorizationSessionReaderServiceAccount.Name()},
		// Without the TokenReview a token's claims would name the reader.
		{name: "noop authorization admits no reader", mode: env.AuthModeTrustedProxy, reader: "workspace-manager/workspace-manager", wantConfigError: env.AuthorizationSessionReaderServiceAccount.Name()},
		{name: "insecure authentication admits no reader", mode: env.AuthModeInsecure, reader: "workspace-manager/workspace-manager", reviews: fakeTokenReviewer{}, wantConfigError: env.AuthorizationSessionReaderServiceAccount.Name()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			authenticator, err := controllerAuthenticator(tt.mode, "email", "", tt.reviews, "kagent", tt.reader)
			if tt.wantConfigError != "" {
				require.ErrorContains(t, err, tt.wantConfigError)
				require.Nil(t, authenticator)
				return
			}
			require.NoError(t, err)
			session, err := authenticator.Authenticate(auth.AdmitSessionReader(context.Background()), headers, url.Values{})
			require.NoError(t, err)
			require.IsType(t, auth.SessionReaderSession{}, session)
			_, err = authenticator.Authenticate(context.Background(), headers, url.Values{})
			require.ErrorIs(t, err, auth.ErrSessionReaderRefused)
		})
	}
}

func TestControllerAuthenticatorNeedsAServiceAccountTokenAudience(t *testing.T) {
	authenticator, err := controllerAuthenticator(env.AuthModeTrustedProxy, "email", "", fakeTokenReviewer{}, "", "")
	require.ErrorContains(t, err, env.AuthorizationServiceAccountTokenAudience.Name())
	require.Nil(t, authenticator)
}

func TestControllerAuthorizer(t *testing.T) {
	for _, tt := range []struct {
		name, mode, selector string
		want                 any
		wantErr              string
	}{
		{name: "default", mode: env.AuthorizationMode.DefaultValue(), want: &auth.NoopAuthorizer{}},
		{name: "kubernetes", mode: env.AuthorizationModeKubernetes, want: &auth.KubernetesAuthorizer{}},
		{name: "kubernetes with a selector", mode: env.AuthorizationModeKubernetes, selector: "tenant=true", want: &auth.KubernetesAuthorizer{}},
		{name: "invalid selector", mode: env.AuthorizationModeKubernetes, selector: "tenant in", wantErr: env.AuthorizationNamespaceSelector.Name()},
		{name: "unsupported mode", mode: "opa", wantErr: env.AuthorizationMode.Name()},
		{name: "empty mode", mode: "", wantErr: env.AuthorizationMode.Name()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			authorizer, err := controllerAuthorizer(tt.mode, authorizationConfig{namespaceSelector: tt.selector}, fake.NewSimpleClientset())
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Nil(t, authorizer)
				return
			}
			require.NoError(t, err)
			require.IsType(t, tt.want, authorizer)
		})
	}
}

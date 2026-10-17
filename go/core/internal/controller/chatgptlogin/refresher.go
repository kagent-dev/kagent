// Package chatgptlogin keeps the ChatGPT logins of ModelConfigs current: it is
// the one refresher of each login's tokens.
//
// A ChatGPT login is a short-lived access token, a refresh token that rotates
// on every use and the ChatGPT account id. The egress gateway sends the access
// token from the login's Secret; no actor holds the refresh token. Two
// refreshers on one login would invalidate each other's refresh token, so the
// login is refreshed here only: the reconciler is keyed by the Secret, so two
// ModelConfigs on one Secret share one refresh, and controller-runtime runs it
// on the elected leader alone.
package chatgptlogin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// TokenURL is the OpenAI OAuth token endpoint the Codex CLI refreshes its
	// ChatGPT login at.
	TokenURL = "https://auth.openai.com/oauth/token"
	// ClientID is the Codex CLI's OAuth client, the client a ChatGPT login
	// from `codex login` belongs to.
	ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	// maxLead bounds how long before its expiry an access token is refreshed;
	// it covers the gateway's credential cache (up to five minutes) with room
	// for a failed attempt to be retried.
	maxLead = 30 * time.Minute
	// minRetry is the soonest a reconcile comes back to a login.
	minRetry = time.Minute
)

// Refresher refreshes the access token of every Secret a ChatGPT-login
// ModelConfig names before it expires, and writes the new access token and
// the rotated refresh token back into the same Secret.
type Refresher struct {
	kube     client.Client
	secrets  client.Reader
	http     *http.Client
	tokenURL string
	clientID string
	now      func() time.Time
}

// NewRefresher returns a Refresher writing through kube and reading Secrets
// through secrets, which should be uncached: a cache of every Secret would be
// held for the few that are logins.
func NewRefresher(kube client.Client, secrets client.Reader, httpClient *http.Client, tokenURL, clientID string) *Refresher {
	return &Refresher{kube: kube, secrets: secrets, http: httpClient, tokenURL: tokenURL, clientID: clientID, now: time.Now}
}

// SetupWithManager registers the refresher, keyed by the Secret each
// ChatGPT-login ModelConfig names.
func (r *Refresher) SetupWithManager(mgr manager.Manager) error {
	return builder.ControllerManagedBy(mgr).
		Named("chatgpt-login-refresher").
		Watches(&v1alpha3.ModelConfig{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, object client.Object) []reconcile.Request {
			model, ok := object.(*v1alpha3.ModelConfig)
			if !ok || !model.Spec.ChatGPTOAuth() || model.Spec.APIKeySecret == "" {
				return nil
			}
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: model.Namespace, Name: model.Spec.APIKeySecret}}}
		})).
		Complete(r)
}

// Reconcile refreshes the login in the Secret req names once its access
// token is due, and comes back when the next one is.
func (r *Refresher) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("secret", req.NamespacedName)
	used, err := r.isLogin(ctx, req.NamespacedName)
	if err != nil || !used {
		return reconcile.Result{}, err
	}
	secret := &corev1.Secret{}
	if err := r.secrets.Get(ctx, req.NamespacedName, secret); err != nil {
		// A missing Secret is the ModelConfig's ResolvedRefs failure; its
		// creation updates no ModelConfig, so look again later.
		if apierrors.IsNotFound(err) {
			return reconcile.Result{RequeueAfter: maxLead}, nil
		}
		return reconcile.Result{}, err
	}
	// An access token without a readable expiry is never refreshed on a
	// guess: each refresh spends the refresh token, so a guess repeated on
	// every retry would churn the login.
	due, ok := RefreshAt(secret.Data[v1alpha3.ChatGPTAccessTokenKey])
	if !ok {
		return reconcile.Result{}, fmt.Errorf("ChatGPT login Secret %s: %s is not a JWT with an expiry", req.NamespacedName, v1alpha3.ChatGPTAccessTokenKey)
	}
	now := r.now()
	if now.Before(due) {
		return reconcile.Result{RequeueAfter: due.Sub(now)}, nil
	}
	refreshToken := string(secret.Data[v1alpha3.ChatGPTRefreshTokenKey])
	if refreshToken == "" {
		return reconcile.Result{}, fmt.Errorf("ChatGPT login Secret %s has no %s", req.NamespacedName, v1alpha3.ChatGPTRefreshTokenKey)
	}
	tokens, err := r.refresh(ctx, refreshToken)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("refresh ChatGPT login %s: %w", req.NamespacedName, err)
	}
	updated := secret.DeepCopy()
	updated.Data[v1alpha3.ChatGPTAccessTokenKey] = []byte(tokens.AccessToken)
	if tokens.RefreshToken != "" {
		updated.Data[v1alpha3.ChatGPTRefreshTokenKey] = []byte(tokens.RefreshToken)
	}
	// The update carries the read's resourceVersion: a login written in the
	// meantime (a person signing in again) wins over this refresh.
	if err := r.kube.Update(ctx, updated); err != nil {
		if apierrors.IsConflict(err) {
			logger.Info("ChatGPT login changed while it was refreshed; keeping the newer login")
			return reconcile.Result{RequeueAfter: minRetry}, nil
		}
		return reconcile.Result{}, fmt.Errorf("store refreshed ChatGPT login %s: %w", req.NamespacedName, err)
	}
	next, _ := RefreshAt([]byte(tokens.AccessToken))
	logger.Info("refreshed ChatGPT login", "nextRefresh", next)
	return reconcile.Result{RequeueAfter: max(next.Sub(now), minRetry)}, nil
}

// isLogin reports whether a ModelConfig in the Secret's namespace uses it as
// a ChatGPT login.
func (r *Refresher) isLogin(ctx context.Context, secret types.NamespacedName) (bool, error) {
	models := &v1alpha3.ModelConfigList{}
	if err := r.kube.List(ctx, models, client.InNamespace(secret.Namespace)); err != nil {
		return false, err
	}
	for _, model := range models.Items {
		if model.Spec.ChatGPTOAuth() && model.Spec.APIKeySecret == secret.Name {
			return true, nil
		}
	}
	return false, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (r *Refresher) refresh(ctx context.Context, refreshToken string) (tokenResponse, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {r.clientID}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := r.http.Do(request)
	if err != nil {
		return tokenResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return tokenResponse{}, err
	}
	if response.StatusCode != http.StatusOK {
		// The error body names the reason (an expired, reused or revoked
		// refresh token) and carries no token.
		return tokenResponse{}, fmt.Errorf("token endpoint answered %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var tokens tokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		return tokenResponse{}, fmt.Errorf("decode token response: %w", err)
	}
	if tokens.AccessToken == "" {
		return tokenResponse{}, errors.New("token response carries no access token")
	}
	return tokens, nil
}

// RefreshAt is when an access token is due for refresh: maxLead before its
// expiry, or a quarter of its lifetime before when that is shorter. It
// reports false for a token whose expiry cannot be read.
func RefreshAt(accessToken []byte) (time.Time, bool) {
	claims, ok := jwtClaims(accessToken)
	if !ok || claims.Exp == 0 {
		return time.Time{}, false
	}
	expiry := time.Unix(claims.Exp, 0)
	lead := maxLead
	if claims.Iat != 0 && claims.Iat < claims.Exp {
		lead = min(lead, expiry.Sub(time.Unix(claims.Iat, 0))/4)
	}
	return expiry.Add(-lead), true
}

type claims struct {
	Exp int64 `json:"exp"`
	Iat int64 `json:"iat"`
}

// jwtClaims reads a JWT's expiry claims without verifying its signature: the
// claims only schedule the refresh, the token is judged by the backend.
func jwtClaims(token []byte) (claims, bool) {
	parts := strings.Split(string(token), ".")
	if len(parts) != 3 {
		return claims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims{}, false
	}
	var c claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return claims{}, false
	}
	return c, true
}

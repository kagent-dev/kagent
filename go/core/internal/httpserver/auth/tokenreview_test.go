package auth

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	typedauth "k8s.io/client-go/kubernetes/typed/authentication/v1"
)

func TestTokenReviewCacheLifetime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var denied, unavailable bool
		reviewer := &tokenReviewer{create: func(_ context.Context, req *authenticationv1.TokenReview) (*authenticationv1.TokenReview, error) {
			if unavailable {
				return nil, errors.New("sensitive response")
			}
			result := acceptedReview(req)
			result.Status.Authenticated = !denied
			return result, nil
		}}
		p := NewServiceAccountProvider(&InsecureAuthenticator{}, reviewer, "cluster")
		token := reviewToken(t, jwt.MapClaims{}) // Legacy tokens may have no expiry.
		first, err := p.verify(t.Context(), token, serviceAccountAudience)
		require.NoError(t, err)
		// Cached identity must not be writable through a caller's claims.
		first.Claims["username"] = "forged"
		first.Claims["groups"].([]any)[0] = "forged"
		time.Sleep(20 * time.Second)
		denied = true
		second, err := p.verify(t.Context(), token, serviceAccountAudience)
		require.NoError(t, err, "revocation is observed after the cache TTL")
		require.Equal(t, "system:serviceaccount:ns:caller", second.Claims["username"])
		require.Equal(t, []any{"system:serviceaccounts"}, second.Claims["groups"])
		require.EqualValues(t, 1, reviewer.calls.Load())
		time.Sleep(10 * time.Second)
		_, err = p.verify(t.Context(), token, serviceAccountAudience)
		require.ErrorIs(t, err, ErrUnauthenticated, "hits must not extend the 30-second TTL")
		require.EqualValues(t, 2, reviewer.calls.Load())
		denied = false
		_, err = p.verify(t.Context(), token, serviceAccountAudience)
		require.NoError(t, err, "denials must not be cached")
		require.EqualValues(t, 3, reviewer.calls.Load())
		time.Sleep(30 * time.Second)
		unavailable = true
		for range 2 {
			_, err = p.verify(t.Context(), token, serviceAccountAudience)
			require.ErrorContains(t, err, "verification unavailable")
			require.NotContains(t, err.Error(), "sensitive")
		}
		require.EqualValues(t, 5, reviewer.calls.Load(), "API errors must neither be cached nor revive stale success")
		unavailable = false
		_, err = p.verify(t.Context(), token, serviceAccountAudience)
		require.NoError(t, err)
		require.EqualValues(t, 6, reviewer.calls.Load())
	})
}

func TestTokenReviewCacheExpiry(t *testing.T) {
	for _, tc := range []struct {
		name       string
		expiration any
		reviewTime time.Duration
	}{
		{name: "expires while cached", expiration: 5 * time.Second},
		{name: "expires during review", expiration: time.Second, reviewTime: 2 * time.Second},
		{name: "malformed expiry", expiration: "tomorrow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				reviewer := &tokenReviewer{create: func(_ context.Context, req *authenticationv1.TokenReview) (*authenticationv1.TokenReview, error) {
					time.Sleep(tc.reviewTime)
					return acceptedReview(req), nil
				}}
				p := NewServiceAccountProvider(&InsecureAuthenticator{}, reviewer, "cluster")
				exp := tc.expiration
				if ttl, ok := exp.(time.Duration); ok {
					exp = time.Now().Add(ttl).Unix()
				}
				token := reviewToken(t, jwt.MapClaims{"exp": exp})
				_, err := p.verify(t.Context(), token, serviceAccountAudience)
				if tc.name == "expires while cached" {
					require.NoError(t, err)
					time.Sleep(5 * time.Second)
					_, err = p.verify(t.Context(), token, serviceAccountAudience)
				}
				require.ErrorIs(t, err, ErrUnauthenticated)
				if tc.name == "malformed expiry" {
					require.Zero(t, reviewer.calls.Load())
				} else {
					require.EqualValues(t, 1, reviewer.calls.Load())
				}
			})
		})
	}
}

func TestTokenReviewCacheAudienceAndRotation(t *testing.T) {
	reviewer := &tokenReviewer{create: func(_ context.Context, req *authenticationv1.TokenReview) (*authenticationv1.TokenReview, error) {
		result := acceptedReview(req)
		result.Status.Authenticated = req.Spec.Audiences[0] == serviceAccountAudience
		return result, nil
	}}
	p := NewServiceAccountProvider(&InsecureAuthenticator{}, reviewer, "cluster")
	token := reviewToken(t, jwt.MapClaims{"generation": 1})
	first, err := p.verify(t.Context(), token, serviceAccountAudience)
	require.NoError(t, err)
	_, err = p.verify(t.Context(), token, "downstream")
	require.ErrorIs(t, err, ErrUnauthenticated, "a cached inbound review must not authorize a different audience")
	require.EqualValues(t, 2, reviewer.calls.Load())
	rotated := reviewToken(t, jwt.MapClaims{"generation": 2})
	second, err := p.verify(t.Context(), rotated, serviceAccountAudience)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.EqualValues(t, 3, reviewer.calls.Load(), "rotation must verify the new token immediately")
}

func TestTokenReviewCacheConcurrentCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		reviewer := &tokenReviewer{create: func(ctx context.Context, req *authenticationv1.TokenReview) (*authenticationv1.TokenReview, error) {
			select {
			case <-release:
				return acceptedReview(req), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}}
		p := NewServiceAccountProvider(&InsecureAuthenticator{}, reviewer, "cluster")
		token := reviewToken(t, jwt.MapClaims{})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		first := make(chan error, 1)
		go func() {
			_, err := p.verify(ctx, token, serviceAccountAudience)
			first <- err
		}()
		synctest.Wait() // The first caller owns the pending lookup.
		const waiters = 20
		results := make(chan error, waiters)
		for range waiters {
			go func() {
				_, err := p.verify(t.Context(), token, serviceAccountAudience)
				results <- err
			}()
		}
		synctest.Wait()
		cancel()
		require.ErrorIs(t, <-first, context.Canceled)
		close(release)
		for range waiters {
			require.NoError(t, <-results, "one canceled caller must not cancel the shared lookup")
		}
		require.EqualValues(t, 1, reviewer.calls.Load())
	})
}

func TestTokenReviewLookupTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reviewer := &tokenReviewer{create: func(ctx context.Context, _ *authenticationv1.TokenReview) (*authenticationv1.TokenReview, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		p := NewServiceAccountProvider(&InsecureAuthenticator{}, reviewer, "cluster")
		started := time.Now()
		_, err := p.verify(t.Context(), reviewToken(t, jwt.MapClaims{}), serviceAccountAudience)
		require.ErrorContains(t, err, "verification unavailable")
		require.Equal(t, 10*time.Second, time.Since(started))
		require.Zero(t, p.verified.Len())
	})
}

func TestTokenReviewCacheEviction(t *testing.T) {
	reviewer := &tokenReviewer{create: func(_ context.Context, req *authenticationv1.TokenReview) (*authenticationv1.TokenReview, error) {
		return acceptedReview(req), nil
	}}
	p := NewServiceAccountProvider(&InsecureAuthenticator{}, reviewer, "cluster")
	first := reviewToken(t, jwt.MapClaims{"jti": "0"})
	for i := range tokenReviewCacheSize + 1 {
		_, err := p.verify(t.Context(), reviewToken(t, jwt.MapClaims{"jti": fmt.Sprint(i)}), serviceAccountAudience)
		require.NoError(t, err, "a full cache must evict instead of rejecting callers")
	}
	require.Equal(t, tokenReviewCacheSize, p.verified.Len())
	_, err := p.verify(t.Context(), first, serviceAccountAudience)
	require.NoError(t, err)
	require.EqualValues(t, tokenReviewCacheSize+2, reviewer.calls.Load(), "the oldest entry must be reviewed again")
}

type tokenReviewer struct {
	typedauth.TokenReviewInterface
	calls  atomic.Int32
	create func(context.Context, *authenticationv1.TokenReview) (*authenticationv1.TokenReview, error)
}

var _ typedauth.TokenReviewInterface = (*tokenReviewer)(nil)

func (r *tokenReviewer) Create(ctx context.Context, review *authenticationv1.TokenReview, _ metav1.CreateOptions) (*authenticationv1.TokenReview, error) {
	r.calls.Add(1)
	return r.create(ctx, review)
}

func acceptedReview(req *authenticationv1.TokenReview) *authenticationv1.TokenReview {
	return &authenticationv1.TokenReview{Status: authenticationv1.TokenReviewStatus{
		Authenticated: true, Audiences: req.Spec.Audiences,
		User: authenticationv1.UserInfo{Username: "system:serviceaccount:ns:caller", UID: "uid", Groups: []string{"system:serviceaccounts"}},
	}}
}

func reviewToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	claims["sub"] = "system:serviceaccount:ns:caller"
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-key"))
	require.NoError(t, err)
	return token
}

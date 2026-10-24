package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

const (
	tokenReviewCacheSize = 1024
	tokenReviewCacheTTL  = 30 * time.Second
	tokenReviewTimeout   = 10 * time.Second
)

type verifiedServiceAccount struct {
	user       authenticationv1.UserInfo
	validUntil time.Time
}

func (p *ServiceAccountProvider) verify(ctx context.Context, token, audience string) (auth.Principal, error) {
	if err := ctx.Err(); err != nil {
		return auth.Principal{}, err
	}
	digest := sha256.Sum256([]byte(token))
	key := audience + ":" + hex.EncodeToString(digest[:])
	entry, ok := p.cachedReview(key)
	if !ok {
		result := p.lookups.DoChan(key, func() (any, error) {
			if entry, ok := p.cachedReview(key); ok {
				return entry, nil
			}
			// Callers cancel their own wait, not verification shared with others.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tokenReviewTimeout)
			defer cancel()
			entry, err := p.review(ctx, token, audience)
			if err == nil {
				p.verified.Add(key, entry)
			}
			return entry, err
		})
		select {
		case <-ctx.Done():
			return auth.Principal{}, ctx.Err()
		case result := <-result:
			if result.Err != nil {
				return auth.Principal{}, result.Err
			}
			entry = result.Val.(*verifiedServiceAccount)
		}
	}
	if !time.Now().Before(entry.validUntil) {
		return auth.Principal{}, ErrUnauthenticated
	}
	// Each caller owns its claims; mutating them must not affect cached identity.
	// Claims use JSON-compatible values for protobuf Struct encoding.
	groups := make([]any, len(entry.user.Groups))
	for i, group := range entry.user.Groups {
		groups[i] = group
	}
	return auth.Principal{
		ID: auth.PrincipalID("kubernetes:"+p.authority, entry.user.UID),
		Claims: map[string]any{
			"username": entry.user.Username,
			"groups":   groups,
		},
	}, nil
}

func (p *ServiceAccountProvider) cachedReview(key string) (*verifiedServiceAccount, bool) {
	if value, ok := p.verified.Get(key); ok {
		entry := value.(*verifiedServiceAccount)
		if time.Now().Before(entry.validUntil) {
			return entry, true
		}
	}
	return nil, false
}

func (p *ServiceAccountProvider) review(ctx context.Context, token, audience string) (*verifiedServiceAccount, error) {
	claims, err := parseJWTPayload(token)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	expiresAt, err := jwtExpiration(claims)
	if err != nil {
		return nil, err
	}
	// Start the TTL before the request so API latency cannot extend validity.
	validUntil := time.Now().Add(tokenReviewCacheTTL)
	if !expiresAt.IsZero() && expiresAt.Before(validUntil) {
		validUntil = expiresAt
	}
	review, err := p.reviews.Create(ctx, &authenticationv1.TokenReview{Spec: authenticationv1.TokenReviewSpec{Token: token, Audiences: []string{audience}}}, metav1.CreateOptions{})
	// Do not return the API response/error: it may contain credential material.
	if err != nil || review == nil {
		return nil, errors.New("service-account credential verification unavailable")
	}
	s := review.Status
	if !s.Authenticated || s.Error != "" || s.User.UID == "" || !strings.HasPrefix(s.User.Username, "system:serviceaccount:") || !slices.Contains(s.Audiences, audience) || !time.Now().Before(validUntil) {
		return nil, ErrUnauthenticated
	}
	return &verifiedServiceAccount{user: s.User, validUntil: validUntil}, nil
}

package grant_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kagent-dev/kagent/go/core/test/grant"
	"github.com/stretchr/testify/require"
)

const (
	issuer   = "https://workspaces.e2e.test"
	audience = "kagent"
	person   = "person@e2e.test"
)

var (
	workspace = grant.Grant{
		Volume:    grant.Volume{Driver: "nfs.csi.k8s.io", Handle: "nfs-server.kagent.svc#export#workspace-e2e##"},
		Mounts:    []grant.Mount{{SubPath: "sessions/${SESSION_ID}"}, {SubPath: "mirrors", ReadOnly: true}},
		Changed:   []string{"e2e/alpha"},
		Providers: []grant.Provider{{Hostname: "git.e2e.test", Audience: "git", Scheme: "Basic", Username: "x-access-token", Broker: "workspaces"}},
	}
)

// verifier checks a grant the way an admission does: the signature against the
// keys fetched from the issuer's JWKS, the issuer, the audience, the validity
// window and the subject.
type verifier struct {
	jwksURL, issuer, audience string
}

func (v verifier) verify(t *testing.T, token, subject string) (*grant.Claims, error) {
	t.Helper()
	claims := &grant.Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (any, error) {
		return v.key(t, token.Header["kid"])
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodES256.Alg()}),
		jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience), jwt.WithSubject(subject),
		jwt.WithExpirationRequired(),
	)
	return claims, err
}

func (v verifier) key(t *testing.T, kid any) (*ecdsa.PublicKey, error) {
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, v.jwksURL, nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var keys grant.JWKS
	require.NoError(t, json.NewDecoder(response.Body).Decode(&keys))
	for _, key := range keys.Keys {
		if key.KeyID != kid || key.KeyType != "EC" || key.Curve != "P-256" {
			continue
		}
		x, err := base64.RawURLEncoding.DecodeString(key.X)
		require.NoError(t, err)
		y, err := base64.RawURLEncoding.DecodeString(key.Y)
		require.NoError(t, err)
		return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, x...), y...))
	}
	return nil, fmt.Errorf("no key %v in the JWKS", kid)
}

func TestSignedGrantVerifiesAgainstItsJWKS(t *testing.T) {
	signer, err := grant.NewSigner(issuer, audience)
	require.NoError(t, err)
	verify := verifier{jwksURL: signer.Serve(t), issuer: issuer, audience: audience}

	t.Run("valid", func(t *testing.T) {
		token, err := signer.SignGrant(person, workspace, time.Minute)
		require.NoError(t, err)
		claims, err := verify.verify(t, token, person)
		require.NoError(t, err)
		require.Equal(t, workspace, claims.Grant)
	})

	for _, test := range []struct {
		name    string
		signer  *grant.Signer
		subject string
		ttl     time.Duration
		want    error
	}{
		{name: "expired", subject: person, ttl: -time.Minute, want: jwt.ErrTokenExpired},
		{name: "another subject", subject: "someone-else@e2e.test", ttl: time.Minute, want: jwt.ErrTokenInvalidSubject},
		{name: "another issuer's key", signer: mustSigner(t), subject: person, ttl: time.Minute, want: jwt.ErrTokenUnverifiable},
	} {
		t.Run(test.name, func(t *testing.T) {
			issuing := signer
			if test.signer != nil {
				issuing = test.signer
			}
			token, err := issuing.SignGrant(test.subject, workspace, test.ttl)
			require.NoError(t, err)
			_, err = verify.verify(t, token, person)
			require.Error(t, err)
			require.True(t, errors.Is(err, test.want), "want %v, got %v", test.want, err)
		})
	}
}

func mustSigner(t *testing.T) *grant.Signer {
	t.Helper()
	signer, err := grant.NewSigner(issuer, audience)
	require.NoError(t, err)
	return signer
}

package substrate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
)

func TestActorJWTVerifier(t *testing.T) {
	i := jwttest.New(t)
	v, err := NewActorJWTVerifier(t.Context(), i.URL, i.CAFile, RuntimeJWTAudience)
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		change func(jwt.MapClaims)
	}{
		{"wrong issuer", func(c jwt.MapClaims) { c["iss"] = "https://untrusted.example" }},
		{"wrong audience", func(c jwt.MapClaims) { c["aud"] = []string{"another-service"} }},
		{"missing audience", func(c jwt.MapClaims) { delete(c, "aud") }},
		{"expired", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() }},
		{"missing expiration", func(c jwt.MapClaims) { delete(c, "exp") }},
		{"not yet valid", func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(time.Minute).Unix() }},
		{"future issuance", func(c jwt.MapClaims) { c["iat"] = time.Now().Add(time.Minute).Unix() }},
		{"missing issuance", func(c jwt.MapClaims) { delete(c, "iat") }},
		{"missing not-before", func(c jwt.MapClaims) { delete(c, "nbf") }},
		{"wrong subject", func(c jwt.MapClaims) { c["sub"] = "actor/team-a/another-actor" }},
		{"missing actor", func(c jwt.MapClaims) { delete(c, "actor.ate.dev") }},
		{"missing UID", func(c jwt.MapClaims) { delete(c["actor.ate.dev"].(map[string]string), "actorUID") }},
		{"worker token", func(c jwt.MapClaims) {
			c["sub"] = "worker-for-actor/team-a/session-example"
			c["worker-for-actor.ate.dev"] = c["actor.ate.dev"]
			delete(c, "actor.ate.dev")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := i.Claims("team-a", "session-example", "actor-uid")
			test.change(claims)
			_, err := v.Verify(t.Context(), i.Token(t, claims))
			require.Error(t, err)
		})
	}
	claims := i.Claims("team-a", "session-example", "actor-uid")
	raw := i.Token(t, claims)
	identity, err := v.Verify(t.Context(), raw)
	require.NoError(t, err)
	require.Equal(t, ActorIdentity{Atespace: "team-a", ActorName: "session-example", ActorUID: "actor-uid"}, identity)
	other := jwttest.New(t)
	_, err = v.Verify(t.Context(), other.Token(t, claims))
	require.Error(t, err, "a matching kid from another signer must not authenticate")
	for _, method := range []jwt.SigningMethod{jwt.SigningMethodNone, jwt.SigningMethodHS256} {
		token := jwt.NewWithClaims(method, claims)
		token.Header["kid"] = "test-key"
		var key any = []byte("secret")
		if method == jwt.SigningMethodNone {
			key = jwt.UnsafeAllowNoneSignatureType
		}
		forged, err := token.SignedString(key)
		require.NoError(t, err)
		_, err = v.Verify(t.Context(), forged)
		require.Error(t, err)
	}
	// A normal JWKS refresh replaces trust, including revocation of old keys.
	rotated, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	i.SetKey(t, &rotated.PublicKey, "test-key")
	_, err = v.keys.Refresh(t.Context(), v.jwksURL)
	require.NoError(t, err)
	_, err = v.Verify(t.Context(), raw)
	require.Error(t, err)
	i.Key = rotated
	_, err = v.Verify(t.Context(), i.Token(t, claims))
	require.NoError(t, err)
}

func TestActorJWTVerifierRequiresTrustedDiscovery(t *testing.T) {
	i := jwttest.New(t)
	_, err := NewActorJWTVerifier(t.Context(), i.URL, "", RuntimeJWTAudience)
	require.Error(t, err, "untrusted TLS certificate")
	for _, issuer := range []string{"", "http://issuer.test", "https://user:password@issuer.test", "https://issuer.test#fragment", "https://issuer.test?query"} {
		_, err := NewActorJWTVerifier(t.Context(), issuer, i.CAFile, RuntimeJWTAudience)
		require.Error(t, err, issuer)
	}
}

func TestActorJWTVerifierAudienceViews(t *testing.T) {
	i := jwttest.New(t)
	v, err := NewActorJWTVerifier(t.Context(), i.URL, i.CAFile, RuntimeJWTAudience)
	require.NoError(t, err)
	metadata := v.ForAudience("kagent-agent-info")
	require.Same(t, v.keys, metadata.keys, "audience views share signing-key refresh and revocation")
	claims := i.Claims("team-a", "session-example", "actor-uid")
	runtimeToken := i.Token(t, claims)
	claims["aud"] = []string{"downstream-api", "kagent-agent-info"}
	metadataToken := i.Token(t, claims)
	_, err = v.Verify(t.Context(), metadataToken)
	require.Error(t, err)
	_, err = metadata.Verify(t.Context(), runtimeToken)
	require.Error(t, err)
	_, err = metadata.Verify(t.Context(), metadataToken)
	require.NoError(t, err)
	_, err = v.Verify(t.Context(), runtimeToken)
	require.NoError(t, err, "binding another audience must not modify the runtime verifier")
	_, err = v.ForAudience("").Verify(t.Context(), runtimeToken)
	require.Error(t, err)
}

func TestActorJWTVerifierClockSkew(t *testing.T) {
	i := jwttest.New(t)
	v, err := NewActorJWTVerifier(t.Context(), i.URL, i.CAFile, RuntimeJWTAudience)
	require.NoError(t, err)
	for _, test := range []struct {
		name    string
		claim   string
		offset  time.Duration
		wantErr error
	}{
		{name: "recent expiration", claim: "exp", offset: -15 * time.Second},
		{name: "expired beyond tolerance", claim: "exp", offset: -45 * time.Second, wantErr: jwt.ErrTokenExpired},
		{name: "issuance slightly ahead", claim: "iat", offset: 15 * time.Second},
		{name: "issuance beyond tolerance", claim: "iat", offset: 45 * time.Second, wantErr: jwt.ErrTokenUsedBeforeIssued},
		{name: "not-before slightly ahead", claim: "nbf", offset: 15 * time.Second},
		{name: "not-before beyond tolerance", claim: "nbf", offset: 45 * time.Second, wantErr: jwt.ErrTokenNotValidYet},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := i.Claims("team-a", "session-example", "actor-uid")
			now := time.Now()
			claims["iat"] = now.Add(-5 * time.Minute).Unix()
			claims[test.claim] = now.Add(test.offset).Unix()
			identity, err := v.Verify(t.Context(), i.Token(t, claims))
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				require.Empty(t, identity)
				return
			}
			require.NoError(t, err)
			require.Equal(t, ActorIdentity{Atespace: "team-a", ActorName: "session-example", ActorUID: "actor-uid"}, identity)
		})
	}
}

package substrate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/jwk"

	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const (
	runtimeJWTAudience  = "kagent-runtime"
	runtimeJWTClockSkew = 30 * time.Second
)

// ActorIdentity is the identity asserted by Substrate's actor.ate.dev claim.
// Worker-for-actor tokens deliberately use a different claim and subject.
type ActorIdentity struct {
	Atespace  string `json:"atespace"`
	ActorName string `json:"actorName"`
	ActorUID  string `json:"actorUID"`
}

type actorClaims struct {
	jwt.RegisteredClaims
	Actor ActorIdentity `json:"actor.ate.dev"`
}

// ActorJWTVerifier trusts one configured issuer and refreshes its signing keys.
type ActorJWTVerifier struct {
	issuer  string
	jwksURL string
	keys    *jwk.AutoRefresh
}

// NewActorJWTVerifier loads discovery over verified TLS. The context owns key
// refreshes; callers must cancel it when the verifier is no longer used.
func NewActorJWTVerifier(ctx context.Context, issuer, caFile string) (*ActorJWTVerifier, error) {
	if err := validateIssuerURL(issuer); err != nil {
		return nil, err
	}
	tlsConfig, err := ateAPITLSConfig(Config{CAFile: caFile})
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	client := &http.Client{
		Transport: transport, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, fmt.Errorf("create Actor JWT discovery request: %w", err)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch Actor JWT discovery: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("actor JWT discovery returned HTTP %d", response.StatusCode)
	}
	var discovery struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&discovery); err != nil {
		return nil, fmt.Errorf("decode Actor JWT discovery: %w", err)
	}
	if discovery.Issuer != issuer {
		return nil, fmt.Errorf("actor JWT discovery issuer does not match configured issuer")
	}
	if err := validateIssuerURL(discovery.JWKSURI); err != nil {
		return nil, fmt.Errorf("invalid Actor JWT signing key URL: %w", err)
	}
	keys := jwk.NewAutoRefresh(ctx)
	// Refresh on a fixed interval instead of allowing untrusted token kids to
	// trigger network requests. Publish new keys at least a minute before use.
	keys.Configure(discovery.JWKSURI, jwk.WithHTTPClient(client), jwk.WithRefreshInterval(time.Minute))
	if _, err := keys.Refresh(ctx, discovery.JWKSURI); err != nil {
		return nil, fmt.Errorf("load Actor JWT signing keys: %w", err)
	}
	errors := make(chan jwk.AutoRefreshError, 1)
	keys.ErrorSink(errors)
	go func() {
		defer transport.CloseIdleConnections()
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-errors:
				logging.FromContext(ctx).ErrorContext(ctx, "failed to refresh Actor JWT signing keys", "error", err.Error)
			}
		}
	}()
	return &ActorJWTVerifier{issuer: issuer, jwksURL: discovery.JWKSURI, keys: keys}, nil
}

func validateIssuerURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
		return fmt.Errorf("expected an HTTPS issuer URL without credentials, query, or fragment")
	}
	return nil
}

// Verify authenticates a runtime token. Session authorization remains with the
// control-plane service and checks this identity against the recorded Actor UID.
func (v *ActorJWTVerifier) Verify(ctx context.Context, raw string) (ActorIdentity, error) {
	claims := &actorClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("actor JWT requires a signing key ID")
		}
		keys, err := v.keys.Fetch(ctx, v.jwksURL)
		if err != nil {
			return nil, fmt.Errorf("get Actor JWT signing keys: %w", err)
		}
		key, ok := keys.LookupKeyID(kid)
		if !ok || key.Algorithm() != token.Method.Alg() || key.KeyUsage() != "sig" {
			return nil, fmt.Errorf("actor JWT signing key is not trusted")
		}
		var publicKey any
		if err := key.Raw(&publicKey); err != nil {
			return nil, fmt.Errorf("decode Actor JWT signing key: %w", err)
		}
		return publicKey, nil
	}, jwt.WithValidMethods([]string{"ES256", "RS256", "RS384", "RS512"}),
		jwt.WithIssuer(v.issuer), jwt.WithAudience(runtimeJWTAudience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(),
		jwt.WithLeeway(runtimeJWTClockSkew))
	if err != nil {
		return ActorIdentity{}, fmt.Errorf("verify Actor JWT: %w", err)
	}
	a := claims.Actor
	if a.Atespace == "" || a.ActorName == "" || a.ActorUID == "" || strings.ContainsAny(a.Atespace+a.ActorName, "/") ||
		claims.Subject != "actor/"+a.Atespace+"/"+a.ActorName || claims.NotBefore == nil || claims.IssuedAt == nil {
		return ActorIdentity{}, fmt.Errorf("actor JWT has incomplete or inconsistent identity claims")
	}
	return a, nil
}

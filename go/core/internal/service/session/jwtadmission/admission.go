// Package jwtadmission admits a Session volume source with a short-lived JWT
// grant: signed by an issuer the installation trusts, verified against the
// issuer's published JWKS, naming the caller, the volume and the mounts of it
// the Session may have, and carrying what the Session is admitted with. The
// claims that carry the volume, the mounts and the changed paths are named by
// the installation.
package jwtadmission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/service/session"
)

// leeway absorbs the clock skew between the issuer and the controller on exp
// and nbf.
const leeway = 30 * time.Second

// signingMethods are the algorithms a grant may be signed with.
var signingMethods = []string{
	jwt.SigningMethodES256.Alg(), jwt.SigningMethodES384.Alg(),
	jwt.SigningMethodRS256.Alg(), jwt.SigningMethodPS256.Alg(),
}

// Config names the grant issuer an installation trusts and the claims its
// grants carry.
type Config struct {
	// JWKSURL is where the issuer publishes its verification keys.
	JWKSURL string
	// Issuer is the grant's required iss.
	Issuer string
	// Audience is the grant's required aud: this installation's kagent.
	Audience string
	// Claims names the claims that carry what a grant admits. An empty name
	// takes its default from DefaultClaims.
	Claims Claims
}

// Claims names the claims of a grant that carry what it admits.
type Claims struct {
	// Volume is the claim naming the volume: {"driver": ..., "handle": ...}.
	Volume string
	// Mounts is the claim listing the directories of the volume a session may
	// mount: [{"sub_path": ..., "read_only": ...}].
	Mounts string
	// Changed is the optional claim listing paths, relative to the session's
	// working directory, of the repositories whose origin moved since the
	// volume was last synced.
	Changed string
}

// DefaultClaims are the claim names of a grant whose issuer names none.
var DefaultClaims = Claims{Volume: "volume", Mounts: "mounts", Changed: "changed"}

// withDefaults fills every empty name from DefaultClaims.
func (c Claims) withDefaults() Claims {
	if c.Volume == "" {
		c.Volume = DefaultClaims.Volume
	}
	if c.Mounts == "" {
		c.Mounts = DefaultClaims.Mounts
	}
	if c.Changed == "" {
		c.Changed = DefaultClaims.Changed
	}
	return c
}

// validate refuses a name used twice or one of the registered claims the
// admission verifies itself.
func (c Claims) validate() error {
	taken := map[string]bool{"iss": true, "sub": true, "aud": true, "exp": true, "nbf": true, "iat": true, "jti": true}
	for _, name := range []string{c.Volume, c.Mounts, c.Changed} {
		if taken[name] {
			return fmt.Errorf("claim name %q is a registered claim or names two claims", name)
		}
		taken[name] = true
	}
	return nil
}

// Admission verifies workspace grants. It is safe for concurrent use.
type Admission struct {
	issuer, audience string
	claims           Claims
	keys             *keySet
}

// New returns the admission for config. It fetches no key until the first
// grant arrives, so an unreachable issuer never stops the controller.
func New(config Config, client *http.Client) (*Admission, error) {
	location, err := url.Parse(config.JWKSURL)
	if err != nil || (location.Scheme != "https" && location.Scheme != "http") || location.Host == "" {
		return nil, fmt.Errorf("JWKS URL %q is not an absolute http(s) URL", config.JWKSURL)
	}
	if config.Issuer == "" || config.Audience == "" {
		return nil, errors.New("a JWT admission needs an issuer and an audience")
	}
	claims := config.Claims.withDefaults()
	if err := claims.validate(); err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Admission{issuer: config.Issuer, audience: config.Audience, claims: claims, keys: newKeySet(location.String(), client)}, nil
}

// volume is the grant's volume claim: a CSI volume by its driver and handle.
type volume struct {
	Driver string `json:"driver"`
	Handle string `json:"handle"`
}

// mount is a directory of the volume the grant allows, spelled as a volume
// source's sub-path ("sessions/${SESSION_ID}"). A read-write allowance also
// admits the directory read-only.
type mount struct {
	SubPath  string `json:"sub_path"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

// payload is a grant's payload: the registered claims, and every claim by
// name for the ones the installation names.
type payload struct {
	jwt.RegisteredClaims
	named map[string]json.RawMessage
}

func (p *payload) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &p.RegisteredClaims); err != nil {
		return err
	}
	return json.Unmarshal(data, &p.named)
}

// allowance is what a verified payload admits, read from the configured claims.
type allowance struct {
	volume  volume
	mounts  []mount
	changed []string
}

// read decodes the configured claims of p. A missing volume or mounts claim
// reads as none, which admits no source.
func (c Claims) read(p *payload) (allowance, error) {
	var result allowance
	for _, claim := range []struct {
		name string
		into any
	}{
		{c.Volume, &result.volume}, {c.Mounts, &result.mounts}, {c.Changed, &result.changed},
	} {
		raw, ok := p.named[claim.name]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, claim.into); err != nil {
			return allowance{}, serviceerrors.NewPermissionDenied(fmt.Sprintf("admission grant is malformed: claim %q", claim.name), err)
		}
	}
	return result, nil
}

// Admit verifies token and admits caller to a Session on source with the
// grant's changed repositories. Every refusal is PermissionDenied with its
// reason, except an issuer whose keys cannot be fetched, which is Unavailable:
// the grant may well be valid.
func (a *Admission) Admit(ctx context.Context, caller string, source *apiv1alpha1.SessionVolumeSource, token string) (session.Admitted, error) {
	if token == "" {
		return session.Admitted{}, serviceerrors.NewPermissionDenied("no admission grant", nil)
	}
	if source == nil {
		return session.Admitted{}, serviceerrors.NewInvalidArgument("an admission grant admits a volume_source and the request names none", nil)
	}
	verified := &payload{}
	_, err := jwt.ParseWithClaims(token, verified, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, errUnknownKey
		}
		return a.keys.key(ctx, kid)
	},
		jwt.WithValidMethods(signingMethods),
		jwt.WithIssuer(a.issuer), jwt.WithAudience(a.audience),
		jwt.WithExpirationRequired(), jwt.WithLeeway(leeway),
	)
	if err != nil {
		return session.Admitted{}, refusal(err)
	}
	if verified.Subject != caller {
		return session.Admitted{}, serviceerrors.NewPermissionDenied("admission grant names another person", nil)
	}
	granted, err := a.claims.read(verified)
	if err != nil {
		return session.Admitted{}, err
	}
	if err := allows(granted, source); err != nil {
		return session.Admitted{}, err
	}
	return admitted(granted)
}

// refusal maps a failed verification to its named reason, most specific
// first. Key material and the token never reach the message.
func refusal(err error) error {
	for _, reason := range []struct {
		cause   error
		message string
	}{
		{errUnknownKey, "admission grant is signed by an unknown key"},
		{jwt.ErrTokenSignatureInvalid, "admission grant signature is invalid"},
		{jwt.ErrTokenInvalidIssuer, "admission grant is from an unknown issuer"},
		{jwt.ErrTokenInvalidAudience, "admission grant is for another audience"},
		{jwt.ErrTokenExpired, "admission grant expired"},
		{jwt.ErrTokenNotValidYet, "admission grant is not valid yet"},
	} {
		if errors.Is(err, reason.cause) {
			return serviceerrors.NewPermissionDenied(reason.message, err)
		}
	}
	if errors.Is(err, errKeysUnavailable) {
		return serviceerrors.NewUnavailable("admission grant keys are unavailable", err)
	}
	return serviceerrors.NewPermissionDenied("admission grant is malformed", err)
}

// allows refuses a source on another volume than the grant's, or with a mount
// the grant does not allow.
func allows(granted allowance, source *apiv1alpha1.SessionVolumeSource) error {
	if granted.volume.Driver != source.GetVolume().GetCsiDriver() || granted.volume.Handle != source.GetVolume().GetVolumeHandle() {
		return serviceerrors.NewPermissionDenied("admission grant names another volume", nil)
	}
	for _, requested := range source.GetMounts() {
		if !mountAllowed(granted.mounts, requested) {
			access := "read-only"
			if !requested.GetReadOnly() {
				access = "read-write"
			}
			return serviceerrors.NewPermissionDenied(fmt.Sprintf("admission grant does not allow the %s mount of %q", access, requested.GetSubPath()), nil)
		}
	}
	return nil
}

func mountAllowed(allowed []mount, requested *apiv1alpha1.SessionVolumeMount) bool {
	for _, allowance := range allowed {
		if allowance.SubPath == requested.GetSubPath() && (requested.GetReadOnly() || !allowance.ReadOnly) {
			return true
		}
	}
	return false
}

// admitted is what the grant admits the Session with, refused as malformed
// when a changed path is not one a Session can carry.
func admitted(granted allowance) (session.Admitted, error) {
	for _, changed := range granted.changed {
		if changed == "" || changed != path.Clean(changed) || path.IsAbs(changed) || changed == ".." || strings.HasPrefix(changed, "../") {
			return session.Admitted{}, serviceerrors.NewPermissionDenied("admission grant is malformed: a changed repository is not a clean relative path", nil)
		}
	}
	return session.Admitted{Changed: granted.changed}, nil
}

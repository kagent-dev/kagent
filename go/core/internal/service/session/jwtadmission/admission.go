// Package jwtadmission admits a Session volume source with a short-lived JWT
// grant: signed by an issuer the installation trusts, verified against the
// issuer's published JWKS, naming the caller, the volume and the mounts of it
// the Session may have, and carrying what the Session is admitted with.
package jwtadmission

import (
	"context"
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

// Config names the grant issuer an installation trusts.
type Config struct {
	// JWKSURL is where the issuer publishes its verification keys.
	JWKSURL string
	// Issuer is the grant's required iss.
	Issuer string
	// Audience is the grant's required aud: this installation's kagent.
	Audience string
}

// Admission verifies workspace grants. It is safe for concurrent use.
type Admission struct {
	issuer, audience string
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
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Admission{issuer: config.Issuer, audience: config.Audience, keys: newKeySet(location.String(), client)}, nil
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

// provider is a host whose credential the Session gets.
type provider struct {
	Hostname string `json:"hostname"`
	Audience string `json:"audience"`
	Scheme   string `json:"scheme"`
	Username string `json:"username,omitempty"`
	Broker   string `json:"broker"`
}

// claims is a grant's payload.
type claims struct {
	jwt.RegisteredClaims
	Volume    volume     `json:"volume"`
	Mounts    []mount    `json:"mounts"`
	Changed   []string   `json:"changed,omitempty"`
	Providers []provider `json:"providers,omitempty"`
}

// Admit verifies token and admits caller to a Session on source with the
// grant's providers and changed repositories. Every refusal is
// PermissionDenied with its reason, except an issuer whose keys cannot be
// fetched, which is Unavailable: the grant may well be valid.
func (a *Admission) Admit(ctx context.Context, caller string, source *apiv1alpha1.SessionVolumeSource, token string) (session.Admitted, error) {
	if token == "" {
		return session.Admitted{}, serviceerrors.NewPermissionDenied("no admission grant", nil)
	}
	if source == nil {
		return session.Admitted{}, serviceerrors.NewInvalidArgument("an admission grant admits a volume_source and the request names none", nil)
	}
	grant := &claims{}
	_, err := jwt.ParseWithClaims(token, grant, func(token *jwt.Token) (any, error) {
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
	if grant.Subject != caller {
		return session.Admitted{}, serviceerrors.NewPermissionDenied("admission grant names another person", nil)
	}
	if err := allows(grant, source); err != nil {
		return session.Admitted{}, err
	}
	return admitted(grant)
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
func allows(grant *claims, source *apiv1alpha1.SessionVolumeSource) error {
	if grant.Volume.Driver != source.GetVolume().GetCsiDriver() || grant.Volume.Handle != source.GetVolume().GetVolumeHandle() {
		return serviceerrors.NewPermissionDenied("admission grant names another volume", nil)
	}
	for _, requested := range source.GetMounts() {
		if !mountAllowed(grant.Mounts, requested) {
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
// when a provider or a changed path is not one a Session can carry.
func admitted(grant *claims) (session.Admitted, error) {
	result := session.Admitted{Changed: grant.Changed}
	for _, changed := range grant.Changed {
		if changed == "" || changed != path.Clean(changed) || path.IsAbs(changed) || changed == ".." || strings.HasPrefix(changed, "../") {
			return session.Admitted{}, serviceerrors.NewPermissionDenied("admission grant is malformed: a changed repository is not a clean relative path", nil)
		}
	}
	for _, granted := range grant.Providers {
		if granted.Hostname == "" || granted.Audience == "" || granted.Broker == "" ||
			(granted.Scheme != "Bearer" && granted.Scheme != "Basic") ||
			(granted.Scheme == "Basic") != (granted.Username != "") {
			return session.Admitted{}, serviceerrors.NewPermissionDenied("admission grant is malformed: a provider needs a hostname, an audience, a broker and the Bearer or Basic scheme, a user name with Basic only", nil)
		}
		result.Providers = append(result.Providers, &apiv1alpha1.SessionProvider{
			Hostname: granted.Hostname, Audience: granted.Audience, Scheme: granted.Scheme,
			Username: granted.Username, Broker: granted.Broker,
		})
	}
	return result, nil
}

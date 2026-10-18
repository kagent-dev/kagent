// Package jwtadmission admits a Session volume source with a short-lived JWT
// grant: signed by an issuer the installation trusts, verified against the
// issuer's published JWKS, naming the caller, the namespace of the Agent the
// Session may run on, the volume and the mounts of it the Session may have,
// and carrying what the Session is admitted with.
package jwtadmission

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
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
	// JWKSURL is where the issuer publishes its verification keys: an https
	// URL, since whoever can rewrite the keys in transit can sign grants.
	JWKSURL string
	// AllowHTTP admits a plain http JWKSURL, for an in-cluster issuer that
	// serves no TLS yet.
	AllowHTTP bool
	// Issuer is the grant's required iss.
	Issuer string
	// Audience is the grant's required aud: this installation's kagent.
	Audience string
	// CredentialAuthority is the authority of the credential provider the
	// installation's egress gateway resolves a Session's provider credentials
	// through (muster.giantswarm.io on the Giant Swarm platform): a grant names
	// a provider instance, never an authority, so it cannot point a credential
	// at a provider of its choosing. Empty refuses every grant with providers.
	CredentialAuthority string
	// ActorJWTProviders are the provider instances that verify the Actor's
	// own JWT themselves, by instance (muster on the Giant Swarm platform):
	// every Session admitted with a grant gets each as a Bearer provider on
	// its hostname, which needs no credential authority. They are the
	// installation's, never a grant's: a grant naming one is refused.
	ActorJWTProviders map[string]egress.ActorJWTProvider
}

// Admission verifies workspace grants. It is safe for concurrent use.
type Admission struct {
	issuer, audience string
	authority        string
	actorJWT         map[string]egress.ActorJWTProvider
	keys             *keySet
}

// ErrPlainHTTP is New's refusal of a plain http JWKS URL the configuration
// does not allow.
var ErrPlainHTTP = errors.New("the keys could be replaced in transit; use https, or allow http explicitly")

// Refused is the admission a configuration New refused leaves the controller
// with: it refuses every grant with Cause, so a misconfigured admission
// refuses Session volume sources and stops nothing else.
type Refused struct {
	Cause error
}

// Admit refuses the grant: the installation's admission is misconfigured.
func (r Refused) Admit(context.Context, string, *apiv1alpha1.ResourceReference, *apiv1alpha1.SessionVolumeSource, string) (session.Admitted, error) {
	return session.Admitted{}, serviceerrors.NewFailedPrecondition("this installation's Session admission is misconfigured and admits no volume source", r.Cause)
}

// New returns the admission for config. It fetches no key until the first
// grant arrives, so an unreachable issuer never stops the controller.
func New(config Config, client *http.Client) (*Admission, error) {
	location, err := url.Parse(config.JWKSURL)
	if err != nil || (location.Scheme != "https" && location.Scheme != "http") || location.Host == "" {
		return nil, fmt.Errorf("JWKS URL %q is not an absolute https URL", config.JWKSURL)
	}
	if location.Scheme == "http" && !config.AllowHTTP {
		return nil, fmt.Errorf("JWKS URL %q is plain http: %w", config.JWKSURL, ErrPlainHTTP)
	}
	if config.Issuer == "" || config.Audience == "" {
		return nil, errors.New("a JWT admission needs an issuer and an audience")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if err := egress.ValidCredentialAuthority(config.CredentialAuthority); config.CredentialAuthority != "" && err != nil {
		return nil, err
	}
	return &Admission{issuer: config.Issuer, audience: config.Audience, authority: config.CredentialAuthority, actorJWT: config.ActorJWTProviders, keys: newKeySet(location.String(), client)}, nil
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

// provider is a host, or the request paths under PathPrefix on it, whose
// credential the Session gets from the provider instance Provider. Audience
// is the instance of a grant issued before Provider existed.
type provider struct {
	Hostname   string `json:"hostname"`
	PathPrefix string `json:"pathPrefix,omitempty"`
	Scheme     string `json:"scheme"`
	Provider   string `json:"provider"`
	Audience   string `json:"audience,omitempty"`
}

// claims is a grant's payload.
type claims struct {
	jwt.RegisteredClaims
	// AgentNamespace is the namespace of the Agents the grant admits a
	// Session on: a workspace's grant opens its volume and its providers to
	// that namespace's Agents only.
	AgentNamespace string     `json:"agent_namespace"`
	Volume         volume     `json:"volume"`
	Mounts         []mount    `json:"mounts"`
	Changed        []string   `json:"changed,omitempty"`
	Providers      []provider `json:"providers,omitempty"`
}

// Admit verifies token and admits caller to a Session of agent on source with
// the grant's providers and changed repositories. Every refusal is
// PermissionDenied with its reason, except an issuer whose keys cannot be
// fetched, which is Unavailable: the grant may well be valid.
func (a *Admission) Admit(ctx context.Context, caller string, agent *apiv1alpha1.ResourceReference, source *apiv1alpha1.SessionVolumeSource, token string) (session.Admitted, error) {
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
	// A grant without the claim is refused too: it names no namespace, so it
	// would admit a Session on an Agent in any.
	if grant.AgentNamespace == "" || grant.AgentNamespace != agent.GetNamespace() {
		return session.Admitted{}, serviceerrors.NewPermissionDenied("admission grant is for another namespace", nil)
	}
	if err := allows(grant, source); err != nil {
		return session.Admitted{}, err
	}
	return admitted(grant, a.authority, a.actorJWT)
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
// when a provider or a changed path is not one a Session can carry, and
// refused when the grant has providers and the installation configures no
// credential authority, or names a provider the installation configures as
// verifying the Actor's JWT (actorJWT). Each of those joins the Session's
// providers with the Bearer scheme on its own host. A host takes one provider
// per path prefix, so a request has one credential.
func admitted(grant *claims, authority string, actorJWT map[string]egress.ActorJWTProvider) (session.Admitted, error) {
	result := session.Admitted{Changed: grant.Changed}
	for _, changed := range grant.Changed {
		if changed == "" || changed != path.Clean(changed) || path.IsAbs(changed) || changed == ".." || strings.HasPrefix(changed, "../") {
			return session.Admitted{}, serviceerrors.NewPermissionDenied("admission grant is malformed: a changed repository is not a clean relative path", nil)
		}
	}
	if len(grant.Providers) != 0 && authority == "" {
		return session.Admitted{}, serviceerrors.NewPermissionDenied("admission grant names providers and this installation configures no credential authority", nil)
	}
	scopes := map[string]bool{}
	for _, granted := range grant.Providers {
		instance := cmp.Or(granted.Provider, granted.Audience)
		if granted.Hostname == "" || (granted.Scheme != "Bearer" && granted.Scheme != "Basic") {
			return session.Admitted{}, serviceerrors.NewPermissionDenied("admission grant is malformed: a provider needs a hostname and the Bearer or Basic scheme", nil)
		}
		if !egress.ValidProviderInstance(instance) {
			return session.Admitted{}, serviceerrors.NewPermissionDenied(fmt.Sprintf("admission grant is malformed: provider instance %q is not a DNS label", instance), nil)
		}
		if _, configured := actorJWT[instance]; configured {
			return session.Admitted{}, serviceerrors.NewPermissionDenied(fmt.Sprintf("admission grant names provider %q, which this installation configures", instance), nil)
		}
		if granted.PathPrefix != "" && !egress.ValidPathPrefix(granted.PathPrefix) {
			return session.Admitted{}, serviceerrors.NewPermissionDenied(fmt.Sprintf("admission grant is malformed: provider path prefix %q is not a path below the root starting and ending with /", granted.PathPrefix), nil)
		}
		scope := strings.ToLower(granted.Hostname) + granted.PathPrefix
		if scopes[scope] {
			return session.Admitted{}, serviceerrors.NewPermissionDenied(fmt.Sprintf("admission grant is malformed: %s is granted more than once", scope), nil)
		}
		scopes[scope] = true
		result.Providers = append(result.Providers, &apiv1alpha1.SessionProvider{
			Hostname: granted.Hostname, Scheme: granted.Scheme, PathPrefix: granted.PathPrefix,
			//nolint:staticcheck // audience mirrors provider for clients that read it before provider existed.
			Provider: instance, Audience: instance,
		})
	}
	for _, instance := range slices.Sorted(maps.Keys(actorJWT)) {
		hostname := actorJWT[instance].Hostname
		if slices.ContainsFunc(result.Providers, func(p *apiv1alpha1.SessionProvider) bool { return strings.EqualFold(p.GetHostname(), hostname) }) {
			return session.Admitted{}, serviceerrors.NewPermissionDenied(fmt.Sprintf("admission grant names %s, the host of this installation's provider %q", hostname, instance), nil)
		}
		result.Providers = append(result.Providers, &apiv1alpha1.SessionProvider{
			Hostname: hostname, Scheme: "Bearer",
			//nolint:staticcheck // audience mirrors provider for clients that read it before provider existed.
			Provider: instance, Audience: instance,
		})
	}
	return result, nil
}

package env

import "time"

var SessionIdleTTL = RegisterDurationVar("KAGENT_SESSION_IDLE_TTL", 7*24*time.Hour, "Delete sessions after this idle duration. Zero disables expiration; running and waiting tasks are retained.", ComponentController)

var SessionExpirationPollInterval = RegisterDurationVar("KAGENT_SESSION_EXPIRATION_POLL_INTERVAL", time.Minute, "Interval between the expiration sweeps, which delete idle sessions and expired session shares. Must be positive.", ComponentController)

var SessionShareMaxTTL = RegisterDurationVar("KAGENT_SESSION_SHARE_MAX_TTL", 0, "Longest lifetime a session share may request. Shares created without a ttl receive it. Zero leaves shares unbounded.", ComponentController)

var SessionQuiescencePollInterval = RegisterDurationVar("KAGENT_SESSION_QUIESCENCE_POLL_INTERVAL", time.Minute, "Recovery interval for missed session quiescence wake-ups. Must be positive; committed settlements wake local workers immediately and blocked work retries within one second.", ComponentController)
var SessionStalledTurnTimeout = RegisterDurationVar("KAGENT_STALLED_TURN_TIMEOUT", time.Hour, "Fail a submitted or working turn whose runtime recorded no event for this long, so the session takes its next message. The sweep runs on the session expiration poll interval. Zero disables it.", ComponentController)

var SessionRevisionRepointInterval = RegisterDurationVar("KAGENT_REVISION_REPOINT_INTERVAL", 5*time.Minute, "How often the controller moves the quiesced runtimes of sessions whose revision their agent has superseded onto the agent's current revision, so a conversation nobody writes to again does not keep the old revision and its ActorTemplate alive. A turn moves its own runtime regardless. Zero disables the sweep.", ComponentController)

var SessionPausedRuntimeTTL = RegisterDurationVar("KAGENT_PAUSED_RUNTIME_TTL", 2*time.Minute, "Suspend a runtime paused for input once its pause is older than this, so the reply restores it from a durable snapshot instead of the node the pause was taken on. Zero keeps every pause in place until the reply.", ComponentController)

// The jwt Session admission: set all three to admit a create's volume source
// with a grant the issuer signed; leave all unset to refuse every volume source.
var (
	SessionAdmissionJWTJWKSURL   = RegisterStringVar("KAGENT_SESSION_ADMISSION_JWT_JWKS_URL", "", "HTTPS URL of the JWKS that verifies the signed grants a Session volume source is admitted with. Empty, with the issuer and audience, configures no admission: every create that names a volume source or carries an admission token is refused.", ComponentController)
	SessionAdmissionJWTAllowHTTP = RegisterBoolVar("KAGENT_SESSION_ADMISSION_JWT_ALLOW_HTTP", false, "Allow a plain HTTP KAGENT_SESSION_ADMISSION_JWT_JWKS_URL, for an in-cluster issuer that serves no TLS. HTTPS is required by default: a plain HTTP URL without it is logged as an error and every create that names a volume source or carries an admission token is refused; the controller keeps running.", ComponentController)
	SessionAdmissionJWTIssuer    = RegisterStringVar("KAGENT_SESSION_ADMISSION_JWT_ISSUER", "", "Required iss of a Session admission grant. Set with KAGENT_SESSION_ADMISSION_JWT_JWKS_URL.", ComponentController)
	SessionAdmissionJWTAudience  = RegisterStringVar("KAGENT_SESSION_ADMISSION_JWT_AUDIENCE", "", "Required aud of a Session admission grant: this kagent. Set with KAGENT_SESSION_ADMISSION_JWT_JWKS_URL.", ComponentController)
)

// SessionCredentialAuthority is the credential provider a Session's provider
// credentials resolve through.
var SessionCredentialAuthority = RegisterStringVar("KAGENT_SESSION_CREDENTIAL_AUTHORITY", "", "Authority of the egress gateway's credential provider that answers a Session's provider credentials (ate-secret://<authority>/<provider instance>/<git|api>), a DNS name other than a Kubernetes Secret provider. Empty refuses every admission grant that names providers.", ComponentController)

// SessionActorJWTProviders are the Session provider instances that verify the
// Actor's own JWT themselves.
var SessionActorJWTProviders = RegisterStringVar("KAGENT_SESSION_ACTOR_JWT_PROVIDERS", "", "Session provider instances that verify the Actor's own JWT themselves, a JSON object of provider instance to its hostname and audience ({\"muster\":{\"hostname\":\"muster.example.svc\",\"audience\":\"https://muster.example.com\"}}). Every Session gets each as a Bearer provider on its hostname, with or without an admission grant, where the egress gateway replaces the Authorization header with the JWT it mints for the Session's Actor, bound to the audience; it needs no credential authority, and a grant naming the instance or its host is refused. Empty configures none.", ComponentController)

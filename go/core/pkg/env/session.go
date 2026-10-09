package env

import "time"

var SessionIdleTTL = RegisterDurationVar("KAGENT_SESSION_IDLE_TTL", 7*24*time.Hour, "Delete sessions after this idle duration. Zero disables expiration; running and waiting tasks are retained.", ComponentController)

var SessionExpirationPollInterval = RegisterDurationVar("KAGENT_SESSION_EXPIRATION_POLL_INTERVAL", time.Minute, "Interval between idle session expiration sweeps. Must be positive.", ComponentController)

var SessionShareMaxTTL = RegisterDurationVar("KAGENT_SESSION_SHARE_MAX_TTL", 0, "Longest lifetime a session share may request. Shares created without a ttl receive it. Zero leaves shares unbounded.", ComponentController)

var SessionQuiescencePollInterval = RegisterDurationVar("KAGENT_SESSION_QUIESCENCE_POLL_INTERVAL", time.Minute, "Recovery interval for missed session quiescence wake-ups. Must be positive; committed settlements wake local workers immediately and blocked work retries within one second.", ComponentController)

// The jwt Session admission: set the JWKS URL, issuer and audience to admit a
// create's volume source with a grant the issuer signed; leave all three unset
// to refuse every volume source. The claim names follow the issuer's grants.
var (
	SessionAdmissionJWTJWKSURL  = RegisterStringVar("KAGENT_SESSION_ADMISSION_JWT_JWKS_URL", "", "URL of the JWKS that verifies the signed grants a Session volume source is admitted with. Empty, with the issuer and audience, configures no admission: every create that names a volume source or carries an admission token is refused.", ComponentController)
	SessionAdmissionJWTIssuer   = RegisterStringVar("KAGENT_SESSION_ADMISSION_JWT_ISSUER", "", "Required iss of a Session admission grant. Set with KAGENT_SESSION_ADMISSION_JWT_JWKS_URL.", ComponentController)
	SessionAdmissionJWTAudience = RegisterStringVar("KAGENT_SESSION_ADMISSION_JWT_AUDIENCE", "", "Required aud of a Session admission grant: this kagent. Set with KAGENT_SESSION_ADMISSION_JWT_JWKS_URL.", ComponentController)

	SessionAdmissionJWTVolumeClaim  = RegisterStringVar("KAGENT_SESSION_ADMISSION_JWT_VOLUME_CLAIM", "volume", "Claim of a Session admission grant that names the volume, an object with driver and handle.", ComponentController)
	SessionAdmissionJWTMountsClaim  = RegisterStringVar("KAGENT_SESSION_ADMISSION_JWT_MOUNTS_CLAIM", "mounts", "Claim of a Session admission grant that lists the directories of the volume a Session may mount, objects with sub_path and read_only.", ComponentController)
	SessionAdmissionJWTChangedClaim = RegisterStringVar("KAGENT_SESSION_ADMISSION_JWT_CHANGED_CLAIM", "changed", "Optional claim of a Session admission grant that lists the repositories, relative to the Session's working directory, whose origin moved since the volume's last sync.", ComponentController)
)

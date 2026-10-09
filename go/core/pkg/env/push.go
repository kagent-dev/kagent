package env

import "time"

// A2A push notification delivery settings.
var (
	A2APushPollInterval = RegisterDurationVar(
		"KAGENT_A2A_PUSH_POLL_INTERVAL", time.Minute,
		"Recovery interval for missed push notification wake-ups and registration cleanup. Must be positive; settlements wake local workers immediately and outstanding deliveries use a five-second polling interval.",
		ComponentController,
	)

	A2APushAllowPrivateNetworks = RegisterBoolVar(
		"KAGENT_A2A_PUSH_ALLOW_PRIVATE_NETWORKS",
		false,
		"Allow A2A push callbacks to private, loopback, and link-local destinations.",
		ComponentController,
	)
	A2APushAllowHTTP = RegisterBoolVar(
		"KAGENT_A2A_PUSH_ALLOW_HTTP",
		false,
		"Allow HTTP A2A push callbacks. HTTPS is required by default.",
		ComponentController,
	)
	A2APushSigningPrivateKey = RegisterStringVar(
		"KAGENT_A2A_PUSH_SIGNING_PRIVATE_KEY",
		"",
		"Unencrypted PKCS#8 PEM Ed25519 private key shared by controller replicas for push notification JWTs. Supply through a Kubernetes Secret. Empty disables JWT signing.",
		ComponentController,
	)
	A2APushIssuer = RegisterStringVar(
		"KAGENT_A2A_PUSH_ISSUER",
		"",
		"Stable issuer URL for push notification JWTs. Defaults to KAGENT_GATEWAY_URL.",
		ComponentController,
	)
)

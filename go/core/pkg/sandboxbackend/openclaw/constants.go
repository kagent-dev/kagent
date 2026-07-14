package openclaw

const (
	// SubstrateActorHome is the home directory of the unprivileged user in the
	// acp-sandbox openclaw image (USER agent); openclaw.json is written under
	// it. The image ref itself is resolved in the substrate package
	// (substrate constants.go), alongside the other backend images.
	SubstrateActorHome = "/home/agent"

	// substrateSecretProviderID is the env SecretRef provider id for native OpenClaw on Substrate.
	substrateSecretProviderID = "default"

	// SubstrateBootstrapDefaultBaseURL is passed when building openclaw.json for Substrate harnesses.
	// When ModelConfig has no explicit provider URL, the models section is omitted entirely so
	// OpenClaw is not given a partial providers.* block (baseUrl is required when present).
	SubstrateBootstrapDefaultBaseURL = ""
)

package substrate

import "time"

// Config holds connection settings for Agent Substrate ate-api.
type Config struct {
	// AteAPIEndpoint is a gRPC target (e.g. dns:///api.ate-system.svc:443).
	AteAPIEndpoint string
	// Insecure skips TLS certificate verification (local Kind / private CA). ate-api still uses TLS on :443.
	Insecure bool
	// DialTimeout bounds the initial dial. Zero defaults to 10s in Dial.
	DialTimeout time.Duration
	CallTimeout time.Duration

	// DefaultActorTemplateNamespace/name is a legacy fallback when status/spec refs are unset.
	DefaultActorTemplateNamespace string
	DefaultActorTemplateName      string

	// ProvisionDefaults configures auto-created WorkerPool/ActorTemplate resources.
	ProvisionDefaults ProvisionDefaults
}

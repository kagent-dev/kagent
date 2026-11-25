package env

const ComponentAnthropicSandboxWorker Component = "anthropic-sandbox-worker"

var (
	AnthropicSandboxDatabaseRole = RegisterStringVar("ANTHROPIC_SANDBOX_DATABASE_ROLE", "",
		"PostgreSQL role assumed by the adapter after login", ComponentAnthropicSandboxWorker)
	AnthropicSandboxDatabaseURL = RegisterStringVar("ANTHROPIC_SANDBOX_DATABASE_URL", "",
		"PostgreSQL connection string for the standalone sandbox adapter.", ComponentAnthropicSandboxWorker)
	AnthropicSandboxDatabaseSchema = RegisterStringVar("ANTHROPIC_SANDBOX_DATABASE_SCHEMA", "",
		"Dedicated PostgreSQL schema for the standalone sandbox adapter.", ComponentAnthropicSandboxWorker)
	AnthropicSandboxEncryptionKey = RegisterStringVar("ANTHROPIC_SANDBOX_ENCRYPTION_KEY", "",
		"Base64-encoded key protecting persisted assignment credentials.", ComponentAnthropicSandboxWorker)
	AnthropicSandboxKagentURL = RegisterStringVar("ANTHROPIC_SANDBOX_KAGENT_URL", "",
		"In-cluster Kagent controller gRPC API URL.", ComponentAnthropicSandboxWorker)
	AnthropicSandboxNamespace = RegisterStringVar("ANTHROPIC_SANDBOX_NAMESPACE", "",
		"Default namespace for mapped SandboxTemplates.", ComponentAnthropicSandboxWorker)
)

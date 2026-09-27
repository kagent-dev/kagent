package env

// The chart still emits these names, but the current controller has no readers.
// Keep them discoverable without promising that setting them changes behavior.
func init() {
	for _, variable := range []Var{
		{Name: "AUTH_MODE", Description: "Rendered by Helm but ignored by the current controller. Authentication is supplied through app.Options.Authenticator."},
		{Name: "AUTH_USER_ID_CLAIM", Description: "Rendered by Helm but ignored by the current controller. Identity extraction belongs to the configured authenticator."},
		{Name: "GRPC_MAX_MESSAGE_BYTES", Description: "Rendered by Helm but ignored by the current controller; does not change its gRPC message limits."},
		{Name: "GRPC_TLS_CERT_FILE", Description: "Rendered by Helm but ignored by the current controller; does not enable TLS on its listener."},
		{Name: "GRPC_TLS_KEY_FILE", Description: "Rendered by Helm but ignored by the current controller; does not enable TLS on its listener."},
		{Name: "SUBSTRATE_DEFAULT_WORKERPOOL_NAMESPACE", Description: "Rendered by Helm but ignored by the current controller. Worker pools are resolved from Harness runtime configuration."},
		{Name: "SUBSTRATE_DEFAULT_WORKERPOOL_NAME", Description: "Rendered by Helm but ignored by the current controller. Worker pools are resolved from Harness runtime configuration."},
	} {
		variable.Component = ComponentController
		variable.Deprecated = true
		register(variable)
	}
}

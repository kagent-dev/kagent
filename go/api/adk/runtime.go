package adk

// ProcessConfig returns root settings or the standalone zero defaults.
func ProcessConfig(a *Config) *RuntimeConfig {
	if a == nil || a.Runtime == nil {
		return &RuntimeConfig{}
	}
	return a.Runtime
}

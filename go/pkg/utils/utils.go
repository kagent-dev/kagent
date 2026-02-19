package utils

// GetMetadataValue looks up an unprefixed key in A2A metadata, checking
// "adk_<key>" first then falling back to "kagent_<key>".  This allows
// interoperability with upstream ADK (adk_ prefix) while preserving
// backward-compatibility with kagent's own kagent_ prefix.
func GetMetadataValue(metadata map[string]any, key string) (any, bool) {
	if metadata == nil {
		return nil, false
	}
	if v, ok := metadata["adk_"+key]; ok {
		return v, true
	}
	if v, ok := metadata["kagent_"+key]; ok {
		return v, true
	}
	return nil, false
}

package translator

// VertexAIHostname is the Vertex AI endpoint for a ModelConfig location: the
// global endpoint, a multi-region endpoint for us and eu, or the regional one.
func VertexAIHostname(location string) string {
	switch location {
	case "global":
		return "aiplatform.googleapis.com"
	case "us", "eu":
		return "aiplatform." + location + ".rep.googleapis.com"
	default:
		return location + "-aiplatform.googleapis.com"
	}
}

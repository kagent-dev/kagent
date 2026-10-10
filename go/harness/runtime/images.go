package runtime

// OmitImageData returns a tool result with the base64 data of its images
// replaced by their size. The model has already seen them, and stored task
// history would otherwise carry every image a turn reads.
func OmitImageData(value any) any {
	switch value := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, field := range value {
			out[key] = OmitImageData(field)
		}
		if value["type"] == "image" {
			omitData(out)
			if source, ok := out["source"].(map[string]any); ok {
				omitData(source)
			}
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = OmitImageData(item)
		}
		return out
	}
	return value
}

// omitData covers both image shapes: MCP's, with data beside the type, and
// Claude's, with data in the source.
func omitData(image map[string]any) {
	if data, ok := image["data"].(string); ok {
		delete(image, "data")
		image["omitted"] = "image data"
		image["bytes"] = len(data) / 4 * 3
	}
}

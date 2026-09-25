// Package fileextract turns uploaded text-like file blobs into text the model can read.
package fileextract

import (
	"fmt"
	"path/filepath"
	"strings"

	"google.golang.org/genai"
)

// textExts are treated as text when the browser sends no useful MIME type.
var textExts = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".csv": true, ".tsv": true, ".log": true,
	".json": true, ".ndjson": true, ".yaml": true, ".yml": true, ".xml": true, ".toml": true,
	".ini": true, ".conf": true, ".sh": true, ".py": true, ".go": true, ".js": true, ".ts": true,
}

// normalizeMIME strips parameters (e.g. "; charset=utf-8") and lowercases.
func normalizeMIME(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if i := strings.IndexByte(mimeType, ';'); i >= 0 {
		mimeType = strings.TrimSpace(mimeType[:i])
	}
	return mimeType
}

// isTextLikeMIME reports whether a MIME type can be inlined as raw text.
func isTextLikeMIME(mimeType string) bool {
	mimeType = normalizeMIME(mimeType)
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	switch mimeType {
	case "application/json", "application/xml", "application/x-ndjson",
		"application/yaml", "application/x-yaml":
		return true
	}
	return false
}

// extractFileText turns a file's bytes into text, or errors for formats that
// have none (e.g. arbitrary binary).
func extractFileText(data []byte, mimeType, name string) (string, error) {
	if isTextLikeMIME(mimeType) {
		return string(data), nil
	}
	if m := normalizeMIME(mimeType); (m == "" || m == "application/octet-stream") && textExts[strings.ToLower(filepath.Ext(name))] {
		return string(data), nil
	}
	return "", fmt.Errorf("unsupported file type for text extraction: mime=%q name=%q", mimeType, name)
}

// maxTextChars caps the text taken from one file.
const maxTextChars = 200_000

// InlineFileToText converts a non-image file blob named name into chat text.
// Unsupported types become a short note so the model can say the file was unreadable.
func InlineFileToText(blob *genai.Blob, name string) string {
	if blob == nil {
		return ""
	}
	if name == "" {
		name = "file"
	}
	text, err := extractFileText(blob.Data, blob.MIMEType, name)
	if err != nil {
		return fmt.Sprintf("[Uploaded file %q (%s) could not be read as text.]", name, blob.MIMEType)
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Sprintf("[Uploaded file %q (%s) contained no extractable text.]", name, blob.MIMEType)
	}
	if runes := []rune(text); len(runes) > maxTextChars {
		text = string(runes[:maxTextChars]) + "\n\n[truncated]"
	}
	return fmt.Sprintf("Contents of uploaded file %q:\n\n%s", name, text)
}

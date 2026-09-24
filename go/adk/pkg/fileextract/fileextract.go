// Package fileextract turns uploaded file blobs into text the model can read,
// mirroring the Python ADK: rich documents via tabula, text-like files as-is.
package fileextract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tsawler/tabula"
	"google.golang.org/genai"
)

// docMIMEToExt maps rich document MIME types to tabula's format extension.
// Kept in sync with the Python runtime's _file_extract.py.
var docMIMEToExt = map[string]string{
	"application/pdf": ".pdf",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   ".docx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         ".xlsx",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
	"application/epub+zip": ".epub",
	"text/html":            ".html",
}

// docExtractExts is the set of filename extensions that can be extracted.
var docExtractExts = map[string]bool{
	".pdf":  true,
	".docx": true,
	".xlsx": true,
	".pptx": true,
	".epub": true,
	".html": true,
	".htm":  true,
}

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
// text/html is excluded so tabula extracts it instead.
func isTextLikeMIME(mimeType string) bool {
	mimeType = normalizeMIME(mimeType)
	if mimeType == "text/html" {
		return false
	}
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

// docExtractExt returns the tabula extension for a rich document, by filename then
// MIME type. Empty if tabula cannot extract it.
func docExtractExt(mimeType, name string) string {
	if ext := strings.ToLower(filepath.Ext(name)); docExtractExts[ext] {
		if ext == ".htm" {
			return ".html"
		}
		return ext
	}
	if ext, ok := docMIMEToExt[normalizeMIME(mimeType)]; ok {
		return ext
	}
	return ""
}

// extractFileText turns a file's bytes into text, or errors for formats that
// have none (e.g. arbitrary binary).
func extractFileText(data []byte, mimeType, name string) (string, error) {
	if ext := docExtractExt(mimeType, name); ext != "" {
		return extractDocText(data, ext)
	}
	if isTextLikeMIME(mimeType) {
		return string(data), nil
	}
	if m := normalizeMIME(mimeType); (m == "" || m == "application/octet-stream") && textExts[strings.ToLower(filepath.Ext(name))] {
		return string(data), nil
	}
	return "", fmt.Errorf("unsupported file type for text extraction: mime=%q name=%q", mimeType, name)
}

// extractDocText extracts markdown via a temp file, since tabula detects format by
// extension. PDFs go through extractPDF for Type3 fonts and malformed streams.
func extractDocText(data []byte, ext string) (string, error) {
	tmp, err := os.CreateTemp("", "kagent-artifact-*"+ext)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for extraction: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", fmt.Errorf("failed to write temp file for extraction: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file for extraction: %w", err)
	}

	if ext == ".pdf" {
		return extractPDF(tmpName)
	}

	text, _, err := tabula.Open(tmpName).ToMarkdown()
	if err != nil {
		return "", fmt.Errorf("failed to extract text from %s document: %w", ext, err)
	}
	return text, nil
}

// maxTextChars caps extracted text so one upload cannot flood the context window.
const maxTextChars = 200_000

// extract is a seam so tests can make a parser panic.
var extract = extractFileText

// InlineFileToText converts a non-image file blob into chat text. Failures and
// parser panics become a short note so the model can say the file was unreadable.
func InlineFileToText(blob *genai.Blob) (out string) {
	if blob == nil {
		return ""
	}
	name := blob.DisplayName
	if name == "" {
		name = "file"
	}
	unreadable := fmt.Sprintf("[Uploaded file %q (%s) could not be read as text.]", name, blob.MIMEType)
	defer func() {
		if recover() != nil {
			out = unreadable
		}
	}()
	text, err := extract(blob.Data, blob.MIMEType, name)
	if err != nil {
		return unreadable
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Sprintf("[Uploaded file %q (%s) contained no extractable text.]", name, blob.MIMEType)
	}
	if runes := []rune(text); len(runes) > maxTextChars {
		text = string(runes[:maxTextChars]) + "\n\n[truncated]"
	}
	return fmt.Sprintf("Contents of uploaded file %q:\n\n%s", name, text)
}

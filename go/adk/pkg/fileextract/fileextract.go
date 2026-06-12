// Package fileextract turns uploaded file blobs into text the model can read.
//
// Rich documents (PDF, DOCX, XLSX, PPTX, EPUB, HTML) are extracted to
// text/markdown via tabula; PDFs additionally use a ToUnicode-aware fallback
// for Type3 fonts and malformed streams (see pdf.go). Text-like files are
// returned as-is. This mirrors the Python ADK runtime behaviour so non-image
// uploads reach the model instead of being dropped.
package fileextract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tsawler/tabula"
	"google.golang.org/genai"
)

// docMIMEToExt maps rich document MIME types to the file extension used for
// format detection. These are extracted to text/markdown so the model can read
// their contents. The set is kept in sync with the Python runtime
// (_file_extract.py): the common formats both tabula (Go) and markitdown
// (Python) support — PDF, DOCX, XLSX, PPTX, HTML, EPUB.
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

// normalizeMIME strips parameters (e.g. "; charset=utf-8") and lowercases.
func normalizeMIME(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if i := strings.IndexByte(mimeType, ';'); i >= 0 {
		mimeType = strings.TrimSpace(mimeType[:i])
	}
	return mimeType
}

// isTextLikeMIME reports whether a MIME type can be inlined as raw text.
// text/html is excluded so it is routed through tabula extraction instead.
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

// docExtractExt returns the tabula extension to use for a rich document, derived
// from the filename first and the MIME type as a fallback. Empty if the file is
// not a tabula-extractable document.
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

// extractFileText turns an uploaded file's bytes into text the model can read.
// Rich documents (PDF, Office, EPUB, HTML) are extracted to markdown via
// tabula; text-like files are returned as-is. Returns an error for formats that
// cannot be represented as text (e.g. arbitrary binary).
func extractFileText(data []byte, mimeType, name string) (string, error) {
	if ext := docExtractExt(mimeType, name); ext != "" {
		return extractDocText(data, ext)
	}
	if isTextLikeMIME(mimeType) {
		return string(data), nil
	}
	return "", fmt.Errorf("unsupported file type for text extraction: mime=%q name=%q", mimeType, name)
}

// extractDocText writes the bytes to a temp file (tabula detects format by
// extension) and extracts markdown. PDFs are routed through extractPDF, which
// handles Type3 fonts and malformed streams that tabula's markdown path can't.
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

// InlineFileToText converts a non-image inline file blob into text suitable for
// inclusion in a chat message. On extraction failure it returns a short note so
// the model can tell the user the file could not be read, instead of silently
// dropping it. Returns "" for a nil/empty blob.
func InlineFileToText(blob *genai.Blob) string {
	if blob == nil {
		return ""
	}
	name := blob.DisplayName
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
	return fmt.Sprintf("Contents of uploaded file %q:\n\n%s", name, text)
}

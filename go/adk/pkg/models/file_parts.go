package models

import (
	"encoding/base64"
	"fmt"
	"path"
	"strings"

	"google.golang.org/genai"
)

// unsupportedFileNote is appended as text when a file part cannot be mapped
// to a provider-native input. Prefer this over silently dropping the part.
func unsupportedFileNote(name, mime string) string {
	if name == "" {
		name = "unnamed"
	}
	if mime == "" {
		mime = "unknown"
	}
	return fmt.Sprintf("[unsupported file: %s (%s)]", name, mime)
}

func dataURI(mime string, data []byte) string {
	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data))
}

func blobName(b *genai.Blob) string {
	if b == nil {
		return ""
	}
	return b.DisplayName
}

func fileDataName(f *genai.FileData) string {
	if f == nil {
		return ""
	}
	return f.DisplayName
}

func isImageMIME(mime string) bool {
	return strings.HasPrefix(mime, "image/")
}

// OpenAI file support differs by API surface:
//
//   - Chat Completions (`messages[].content[].type=file`, inline file_data): PDF only.
//   - Responses (`input_file`): broad list (txt/md/csv/docx/xlsx/pptx/…).
func isOpenAIPDF(mime, name string) bool {
	if strings.ToLower(mime) == "application/pdf" {
		return true
	}
	return strings.ToLower(path.Ext(name)) == ".pdf"
}

// isOpenAIResponsesFileMIME is the Responses input_file allowlist (common types).
// Link: https://platform.openai.com/docs/guides/file-inputs
func isOpenAIResponsesFileMIME(mime, name string) bool {
	if isOpenAIPDF(mime, name) {
		return true
	}
	switch strings.ToLower(mime) {
	case "text/plain", "text/markdown", "text/csv", "text/tsv", "text/html", "text/xml", "text/css",
		"application/json", "application/xml", "application/rtf", "application/csv", "text/rtf",
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.ms-excel",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-powerpoint",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/vnd.oasis.opendocument.text":
		return true
	}
	if strings.HasPrefix(strings.ToLower(mime), "text/") {
		return true
	}
	// Browsers often send "" / application/octet-stream — use extension.
	switch strings.ToLower(path.Ext(name)) {
	case ".pdf", ".txt", ".text", ".md", ".markdown", ".csv", ".tsv", ".html", ".htm",
		".xml", ".json", ".rtf", ".odt",
		".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".py", ".js", ".mjs", ".ts", ".go", ".java", ".c", ".cc", ".cpp", ".h", ".rb",
		".sh", ".yaml", ".yml", ".css", ".sql":
		return true
	}
	return false
}

func isAnthropicPDF(mime, name string) bool {
	return isOpenAIPDF(mime, name)
}

// isTextFileMIME is true for UTF-8 text we can inline (CC fallback) or send as
// Anthropic PlainTextSource. Binary office formats are excluded.
func isTextFileMIME(mime, name string) bool {
	switch strings.ToLower(mime) {
	case "text/plain", "text/markdown", "text/csv", "text/tsv", "text/html", "text/xml", "text/css",
		"application/json", "application/xml", "application/x-yaml", "text/yaml", "text/x-yaml":
		return true
	}
	if strings.HasPrefix(strings.ToLower(mime), "text/") {
		return true
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".txt", ".text", ".md", ".markdown", ".csv", ".tsv", ".html", ".htm",
		".xml", ".json", ".yaml", ".yml", ".css",
		".py", ".js", ".mjs", ".ts", ".go", ".java", ".c", ".cc", ".cpp", ".h", ".rb", ".sh", ".sql":
		return true
	}
	return false
}

func isAnthropicPlainText(mime, name string) bool {
	return isTextFileMIME(mime, name)
}

// inlineFileText wraps file bytes as a labeled text chunk for APIs that cannot
// take the file natively (Chat Completions non-PDF).
func inlineFileText(name string, data []byte) string {
	if name == "" {
		name = "file"
	}
	return fmt.Sprintf("[file: %s]\n%s", name, string(data))
}

// openAIFilename ensures OpenAI gets a filename (it uses the extension for type detection).
func openAIFilename(name, mime string) string {
	if name != "" {
		return name
	}
	switch strings.ToLower(mime) {
	case "application/pdf":
		return "document.pdf"
	case "text/plain":
		return "document.txt"
	case "text/markdown":
		return "document.md"
	case "text/csv", "application/csv":
		return "document.csv"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "document.docx"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "document.xlsx"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return "document.pptx"
	case "application/json":
		return "document.json"
	default:
		return "document"
	}
}

// bedrockImageFormat maps image MIME → Bedrock ImageFormat. Empty if unsupported.
func bedrockImageFormat(mime string) string {
	switch strings.ToLower(mime) {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpeg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		return ""
	}
}

// bedrockDocumentFormat maps document MIME → Bedrock DocumentFormat. Empty if unsupported.
func bedrockDocumentFormat(mime, name string) string {
	switch strings.ToLower(mime) {
	case "application/pdf":
		return "pdf"
	case "text/csv":
		return "csv"
	case "application/msword":
		return "doc"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "docx"
	case "application/vnd.ms-excel":
		return "xls"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "xlsx"
	case "text/html":
		return "html"
	case "text/plain":
		return "txt"
	case "text/markdown":
		return "md"
	}
	// Fallback: extension from display name.
	switch strings.ToLower(path.Ext(name)) {
	case ".pdf":
		return "pdf"
	case ".csv":
		return "csv"
	case ".doc":
		return "doc"
	case ".docx":
		return "docx"
	case ".xls":
		return "xls"
	case ".xlsx":
		return "xlsx"
	case ".html", ".htm":
		return "html"
	case ".txt":
		return "txt"
	case ".md", ".markdown":
		return "md"
	}
	return ""
}

// bedrockSafeDocName keeps only chars Bedrock accepts in DocumentBlock.Name
// (alphanumeric, single spaces, -()[]; max 200). Neutral renaming is left to callers.
func bedrockSafeDocName(name string) string {
	if name == "" {
		return "document"
	}
	var b strings.Builder
	prevSpace := false
	for _, r := range name {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '(' || r == ')' || r == '[' || r == ']':
			b.WriteRune(r)
			prevSpace = false
		case r == ' ' || r == '_' || r == '.':
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "document"
	}
	if rs := []rune(out); len(rs) > 200 {
		return string(rs[:200])
	}
	return out
}

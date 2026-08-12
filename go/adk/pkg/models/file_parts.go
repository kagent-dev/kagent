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

func isOpenAIFileMIME(mime string) bool {
	return mime == "application/pdf"
}

func isAnthropicPDF(mime string) bool {
	return mime == "application/pdf"
}

func isAnthropicPlainText(mime string) bool {
	return mime == "text/plain" || mime == "text/markdown"
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

// bedrockSafeDocName keeps only chars Bedrock accepts in DocumentBlock.Name.
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
	return out
}

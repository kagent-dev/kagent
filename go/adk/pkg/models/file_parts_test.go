package models

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"google.golang.org/genai"
)

func TestUnsupportedFileNote(t *testing.T) {
	got := unsupportedFileNote("report.pdf", "application/pdf")
	if got != "[unsupported file: report.pdf (application/pdf)]" {
		t.Fatalf("got %q", got)
	}
	if unsupportedFileNote("", "") != "[unsupported file: unnamed (unknown)]" {
		t.Fatalf("empty defaults: %q", unsupportedFileNote("", ""))
	}
}

func TestBedrockDocumentFormat(t *testing.T) {
	tests := []struct {
		mime, name, want string
	}{
		{"application/pdf", "a.pdf", "pdf"},
		{"text/plain", "a.txt", "txt"},
		{"application/zip", "a.zip", ""},
		{"", "notes.md", "md"},
	}
	for _, tt := range tests {
		if got := bedrockDocumentFormat(tt.mime, tt.name); got != tt.want {
			t.Errorf("bedrockDocumentFormat(%q,%q)=%q want %q", tt.mime, tt.name, got, tt.want)
		}
	}
}

func TestGenaiContentsToOpenAIMessages_FileParts(t *testing.T) {
	t.Run("pdf", func(t *testing.T) {
		msgs, _ := genaiContentsToOpenAIMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{Text: "summarize"},
				{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: []byte("%PDF"), DisplayName: "doc.pdf"}},
			},
		}}, nil)
		if len(msgs) != 1 || msgs[0].OfUser == nil {
			t.Fatalf("msgs = %#v", msgs)
		}
		b, err := json.Marshal(msgs[0].OfUser)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if !strings.Contains(s, `"type":"file"`) || !strings.Contains(s, "file_data") || !strings.Contains(s, "application/pdf") {
			t.Fatalf("missing file part: %s", s)
		}
	})

	t.Run("image still works", func(t *testing.T) {
		msgs, _ := genaiContentsToOpenAIMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
			},
		}}, nil)
		b, _ := json.Marshal(msgs[0].OfUser)
		if !strings.Contains(string(b), "image_url") || !strings.Contains(string(b), "data:image/png;base64,") {
			t.Fatalf("missing image: %s", b)
		}
	})

	t.Run("unsupported mime becomes note", func(t *testing.T) {
		msgs, _ := genaiContentsToOpenAIMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: "application/zip", Data: []byte("PK"), DisplayName: "a.zip"}},
			},
		}}, nil)
		b, _ := json.Marshal(msgs[0].OfUser)
		if strings.Contains(string(b), `"type":"file"`) {
			t.Fatalf("should not send zip as file: %s", b)
		}
		if !strings.Contains(string(b), "unsupported file: a.zip") {
			t.Fatalf("missing note: %s", b)
		}
	})

	t.Run("filedata uri unsupported on chat completions", func(t *testing.T) {
		msgs, _ := genaiContentsToOpenAIMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{FileData: &genai.FileData{MIMEType: "application/pdf", FileURI: "https://example.com/a.pdf", DisplayName: "a.pdf"}},
			},
		}}, nil)
		b, _ := json.Marshal(msgs[0].OfUser)
		if !strings.Contains(string(b), "unsupported file: a.pdf") {
			t.Fatalf("missing note: %s", b)
		}
	})
}

func TestGenaiContentsToResponsesInput_FileParts(t *testing.T) {
	t.Run("pdf inline", func(t *testing.T) {
		input, _ := genaiContentsToResponsesInput([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{Text: "read this"},
				{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: []byte("%PDF"), DisplayName: "doc.pdf"}},
			},
		}}, nil)
		b, err := json.Marshal(input[0].OfMessage)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if !strings.Contains(s, "input_file") || !strings.Contains(s, "file_data") {
			t.Fatalf("missing input_file: %s", s)
		}
	})

	t.Run("pdf uri", func(t *testing.T) {
		input, _ := genaiContentsToResponsesInput([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{FileData: &genai.FileData{MIMEType: "application/pdf", FileURI: "https://example.com/a.pdf", DisplayName: "a.pdf"}},
			},
		}}, nil)
		b, _ := json.Marshal(input[0].OfMessage)
		if !strings.Contains(string(b), "file_url") || !strings.Contains(string(b), "https://example.com/a.pdf") {
			t.Fatalf("missing file_url: %s", b)
		}
	})

	t.Run("unsupported mime", func(t *testing.T) {
		input, _ := genaiContentsToResponsesInput([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: "application/zip", Data: []byte("PK"), DisplayName: "a.zip"}},
			},
		}}, nil)
		b, _ := json.Marshal(input[0].OfMessage)
		if strings.Contains(string(b), "input_file") {
			t.Fatalf("should not send zip: %s", b)
		}
		if !strings.Contains(string(b), "unsupported file: a.zip") {
			t.Fatalf("missing note: %s", b)
		}
	})
}

func TestGenaiContentsToAnthropicMessages_FileParts(t *testing.T) {
	t.Run("pdf", func(t *testing.T) {
		msgs, _ := genaiContentsToAnthropicMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{Text: "summarize"},
				{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: []byte("%PDF")}},
			},
		}}, nil)
		if len(msgs) != 1 {
			t.Fatalf("len=%d", len(msgs))
		}
		b, err := json.Marshal(msgs[0])
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if !strings.Contains(s, `"type":"document"`) || !strings.Contains(s, "application/pdf") {
			t.Fatalf("missing document: %s", s)
		}
	})

	t.Run("plain text document", func(t *testing.T) {
		msgs, _ := genaiContentsToAnthropicMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: "text/plain", Data: []byte("hello")}},
			},
		}}, nil)
		b, _ := json.Marshal(msgs[0])
		if !strings.Contains(string(b), `"type":"document"`) {
			t.Fatalf("missing document: %s", b)
		}
	})

	t.Run("pdf uri", func(t *testing.T) {
		msgs, _ := genaiContentsToAnthropicMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{FileData: &genai.FileData{MIMEType: "application/pdf", FileURI: "https://example.com/a.pdf"}},
			},
		}}, nil)
		b, _ := json.Marshal(msgs[0])
		if !strings.Contains(string(b), "https://example.com/a.pdf") {
			t.Fatalf("missing url: %s", b)
		}
	})

	t.Run("image still works", func(t *testing.T) {
		msgs, _ := genaiContentsToAnthropicMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
			},
		}}, nil)
		b, _ := json.Marshal(msgs[0])
		if !strings.Contains(string(b), `"type":"image"`) {
			t.Fatalf("missing image: %s", b)
		}
	})

	t.Run("unsupported mime", func(t *testing.T) {
		msgs, _ := genaiContentsToAnthropicMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: "application/zip", Data: []byte("PK"), DisplayName: "a.zip"}},
			},
		}}, nil)
		b, _ := json.Marshal(msgs[0])
		if strings.Contains(string(b), `"type":"document"`) {
			t.Fatalf("should not send zip: %s", b)
		}
		if !strings.Contains(string(b), "unsupported file: a.zip") {
			t.Fatalf("missing note: %s", b)
		}
	})
}

func TestConvertGenaiContentsToBedrockMessages_FileParts(t *testing.T) {
	t.Run("pdf", func(t *testing.T) {
		msgs, _ := convertGenaiContentsToBedrockMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: []byte("%PDF"), DisplayName: "doc.pdf"}},
			},
		}}, nil, nil)
		if len(msgs) != 1 || len(msgs[0].Content) != 1 {
			t.Fatalf("msgs=%#v", msgs)
		}
		doc, ok := msgs[0].Content[0].(*types.ContentBlockMemberDocument)
		if !ok {
			t.Fatalf("want document, got %T", msgs[0].Content[0])
		}
		if doc.Value.Format != types.DocumentFormatPdf {
			t.Fatalf("format=%q", doc.Value.Format)
		}
	})

	t.Run("image", func(t *testing.T) {
		msgs, _ := convertGenaiContentsToBedrockMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
			},
		}}, nil, nil)
		if _, ok := msgs[0].Content[0].(*types.ContentBlockMemberImage); !ok {
			t.Fatalf("want image, got %T", msgs[0].Content[0])
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		msgs, _ := convertGenaiContentsToBedrockMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: "application/zip", Data: []byte("PK"), DisplayName: "a.zip"}},
			},
		}}, nil, nil)
		text, ok := msgs[0].Content[0].(*types.ContentBlockMemberText)
		if !ok || !strings.Contains(text.Value, "unsupported file: a.zip") {
			t.Fatalf("want text note, got %#v", msgs[0].Content[0])
		}
	})
}

func TestConvertGenaiContentsToOllamaMessages_FileParts(t *testing.T) {
	t.Run("image", func(t *testing.T) {
		msgs, _ := convertGenaiContentsToOllamaMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{Text: "what"},
				{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
			},
		}}, nil)
		if len(msgs) != 1 || len(msgs[0].Images) != 1 {
			t.Fatalf("msgs=%#v", msgs)
		}
	})

	t.Run("pdf note", func(t *testing.T) {
		msgs, _ := convertGenaiContentsToOllamaMessages([]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: []byte("%PDF"), DisplayName: "a.pdf"}},
			},
		}}, nil)
		if len(msgs) != 1 || !strings.Contains(msgs[0].Content, "unsupported file: a.pdf") {
			t.Fatalf("msgs=%#v", msgs)
		}
		if len(msgs[0].Images) != 0 {
			t.Fatalf("should not attach pdf as image")
		}
	})
}

func TestGenaiContentsToOrchTemplate_FileParts(t *testing.T) {
	msgs, _ := genaiContentsToOrchTemplate([]*genai.Content{{
		Role: "user",
		Parts: []*genai.Part{
			{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: []byte("%PDF"), DisplayName: "a.pdf"}},
		},
	}}, nil)
	if len(msgs) != 1 {
		t.Fatalf("msgs=%#v", msgs)
	}
	content, _ := msgs[0]["content"].(string)
	if !strings.Contains(content, "unsupported file: a.pdf") {
		t.Fatalf("content=%q", content)
	}
}

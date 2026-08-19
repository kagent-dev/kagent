package models

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"google.golang.org/genai"
)

func TestGenaiContentsToOpenAIMessages_FileParts(t *testing.T) {
	// Chat Completions: PDF → file part; text → inlined (file_data is PDF-only).
	msgs, _ := genaiContentsToOpenAIMessages([]*genai.Content{{
		Role: "user",
		Parts: []*genai.Part{
			{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: []byte("%PDF"), DisplayName: "doc.pdf"}},
			{InlineData: &genai.Blob{MIMEType: "text/plain", Data: []byte("hello"), DisplayName: "notes.txt"}},
		},
	}}, nil)
	b, err := json.Marshal(msgs[0].OfUser)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"type":"file"`) || !strings.Contains(s, "application/pdf") {
		t.Fatalf("missing pdf file part: %s", s)
	}
	if !strings.Contains(s, "[file: notes.txt]") || !strings.Contains(s, "hello") {
		t.Fatalf("expected inlined txt: %s", s)
	}
}

func TestGenaiContentsToResponsesInput_FileParts(t *testing.T) {
	// Responses input_file accepts PDF and text.
	input, _ := genaiContentsToResponsesInput([]*genai.Content{{
		Role: "user",
		Parts: []*genai.Part{
			{InlineData: &genai.Blob{MIMEType: "text/plain", Data: []byte("hello"), DisplayName: "notes.txt"}},
		},
	}}, nil)
	b, _ := json.Marshal(input[0].OfMessage)
	if !strings.Contains(string(b), "input_file") || !strings.Contains(string(b), "notes.txt") {
		t.Fatalf("missing input_file: %s", b)
	}
}

func TestGenaiContentsToAnthropicMessages_FileParts(t *testing.T) {
	msgs, _ := genaiContentsToAnthropicMessages([]*genai.Content{{
		Role: "user",
		Parts: []*genai.Part{
			{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: []byte("%PDF"), DisplayName: "doc.pdf"}},
		},
	}}, nil)
	b, _ := json.Marshal(msgs[0])
	if !strings.Contains(string(b), `"type":"document"`) {
		t.Fatalf("missing document: %s", b)
	}
}

func TestConvertGenaiContentsToBedrockMessages_FileParts(t *testing.T) {
	// Document-only user turns must also carry a text block (Bedrock Converse API).
	msgs, _ := convertGenaiContentsToBedrockMessages([]*genai.Content{{
		Role: "user",
		Parts: []*genai.Part{
			{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: []byte("%PDF"), DisplayName: "doc.pdf"}},
		},
	}}, nil, nil)
	if len(msgs) != 1 {
		t.Fatalf("msgs=%#v", msgs)
	}
	var hasDoc, hasText bool
	for _, block := range msgs[0].Content {
		switch block.(type) {
		case *types.ContentBlockMemberDocument:
			hasDoc = true
		case *types.ContentBlockMemberText:
			hasText = true
		}
	}
	if !hasDoc || !hasText {
		t.Fatalf("want document+text, got %#v", msgs[0].Content)
	}
}

func TestConvertGenaiContentsToOllamaMessages_FileParts(t *testing.T) {
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
}

func TestGenaiContentsToOrchTemplate_FileParts(t *testing.T) {
	msgs, _ := genaiContentsToOrchTemplate([]*genai.Content{{
		Role: "user",
		Parts: []*genai.Part{
			{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: []byte("%PDF"), DisplayName: "a.pdf"}},
		},
	}}, nil)
	content, _ := msgs[0]["content"].(string)
	if !strings.Contains(content, "unsupported file: a.pdf") {
		t.Fatalf("content=%q", content)
	}
}

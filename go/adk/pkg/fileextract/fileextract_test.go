package fileextract

import (
	"strings"
	"testing"

	"google.golang.org/genai"
)

func TestIsTextLikeMIME(t *testing.T) {
	tests := []struct {
		name string
		mime string
		want bool
	}{
		{name: "plain text", mime: "text/plain", want: true},
		{name: "markdown", mime: "text/markdown", want: true},
		{name: "csv", mime: "text/csv", want: true},
		{name: "json", mime: "application/json", want: true},
		{name: "text with charset", mime: "text/plain; charset=utf-8", want: true},
		{name: "yaml", mime: "application/yaml", want: true},
		{name: "zip not text", mime: "application/zip", want: false},
		{name: "image not text", mime: "image/png", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTextLikeMIME(tt.mime); got != tt.want {
				t.Errorf("isTextLikeMIME(%q) = %v, want %v", tt.mime, got, tt.want)
			}
		})
	}
}

func TestExtractFileText(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		mime     string
		file     string
		wantText string
		wantErr  bool
	}{
		{name: "plain text returned as-is", data: []byte("hello world"), mime: "text/plain", file: "a.txt", wantText: "hello world"},
		{name: "json returned as-is", data: []byte(`{"k":"v"}`), mime: "application/json", file: "a.json", wantText: `{"k":"v"}`},
		{name: "csv returned as-is", data: []byte("a,b\n1,2"), mime: "text/csv", file: "a.csv", wantText: "a,b\n1,2"},
		{name: "markdown returned as-is", data: []byte("# hi"), mime: "text/markdown", file: "a.md", wantText: "# hi"},
		{name: "unsupported binary errors", data: []byte{0x00, 0x01, 0x02}, mime: "application/zip", file: "a.zip", wantErr: true},
		{name: "markdown without mime uses extension", data: []byte("# hi"), mime: "", file: "README.md", wantText: "# hi"},
		{name: "yaml as octet-stream uses extension", data: []byte("k: v"), mime: "application/octet-stream", file: "a.YML", wantText: "k: v"},
		{name: "unknown extension as octet-stream errors", data: []byte{0x00}, mime: "application/octet-stream", file: "a.bin", wantErr: true},
		{name: "extension does not override a binary mime", data: []byte{0x00}, mime: "application/zip", file: "a.txt", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractFileText(tt.data, tt.mime, tt.file)
			if (err != nil) != tt.wantErr {
				t.Fatalf("extractFileText() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.wantText {
				t.Errorf("extractFileText() = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestInlineFileToText(t *testing.T) {
	tests := []struct {
		name     string
		blob     *genai.Blob
		contains string
	}{
		{name: "nil blob", blob: nil, contains: ""},
		{
			name:     "text file labeled with name",
			blob:     &genai.Blob{Data: []byte("line1\nline2"), MIMEType: "text/plain", DisplayName: "notes.txt"},
			contains: `Contents of uploaded file "notes.txt"`,
		},
		{
			name:     "unsupported binary returns note",
			blob:     &genai.Blob{Data: []byte{0x00, 0x01}, MIMEType: "application/zip", DisplayName: "a.zip"},
			contains: "could not be read as text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InlineFileToText(tt.blob)
			if tt.contains == "" {
				if got != "" {
					t.Errorf("InlineFileToText() = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tt.contains) {
				t.Errorf("InlineFileToText() = %q, want containing %q", got, tt.contains)
			}
		})
	}
}

func TestInlineFileToText_Truncates(t *testing.T) {
	data := strings.Repeat("é", maxTextChars+10)
	got := InlineFileToText(&genai.Blob{Data: []byte(data), MIMEType: "text/plain", DisplayName: "big.txt"})
	if !strings.HasSuffix(got, "\n\n[truncated]") {
		t.Fatalf("InlineFileToText() did not note truncation: %q", got[len(got)-40:])
	}
	if n := strings.Count(got, "é"); n != maxTextChars {
		t.Errorf("kept %d characters, want %d", n, maxTextChars)
	}
}

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
		{name: "html excluded", mime: "text/html", want: false},
		{name: "pdf not text", mime: "application/pdf", want: false},
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

func TestDocExtractExt(t *testing.T) {
	tests := []struct {
		name string
		mime string
		file string
		want string
	}{
		{name: "pdf by mime", mime: "application/pdf", file: "report", want: ".pdf"},
		{name: "pdf by extension", mime: "application/octet-stream", file: "report.pdf", want: ".pdf"},
		{name: "docx by mime", mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", file: "x", want: ".docx"},
		{name: "htm normalized to html", mime: "", file: "page.htm", want: ".html"},
		{name: "text not a doc", mime: "text/plain", file: "a.txt", want: ""},
		{name: "unknown", mime: "application/zip", file: "a.zip", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := docExtractExt(tt.mime, tt.file); got != tt.want {
				t.Errorf("docExtractExt(%q, %q) = %q, want %q", tt.mime, tt.file, got, tt.want)
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

// TestExtractFileText_HTML exercises the real tabula path without a binary fixture.
func TestExtractFileText_HTML(t *testing.T) {
	html := []byte(`<html><body><h1>Invoice</h1><p>Total: 42 USD</p></body></html>`)
	got, err := extractFileText(html, "text/html", "invoice.html")
	if err != nil {
		t.Fatalf("extractFileText() error = %v", err)
	}
	if !strings.Contains(got, "Invoice") || !strings.Contains(got, "42 USD") {
		t.Errorf("extracted text missing expected content: %q", got)
	}
}

// TestExtractFileText_Type3PDF: the fixture's ToUnicode CMap maps <0102> to "Hi",
// which tabula's markdown path would render as raw codes.
func TestExtractFileText_Type3PDF(t *testing.T) {
	got, err := extractFileText(type3PDFFixture(), "application/pdf", "type3.pdf")
	if err != nil {
		t.Fatalf("extractFileText() error = %v", err)
	}
	if !strings.Contains(got, "Hi") {
		t.Errorf("Type3 PDF did not decode via ToUnicode; got %q", got)
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

func TestInlineFileToText_ParserPanic(t *testing.T) {
	orig := extract
	t.Cleanup(func() { extract = orig })
	extract = func([]byte, string, string) (string, error) { panic("bad stream") }

	got := InlineFileToText(&genai.Blob{Data: []byte("x"), MIMEType: "application/pdf", DisplayName: "a.pdf"})
	if !strings.Contains(got, "could not be read as text") {
		t.Errorf("InlineFileToText() = %q, want the unreadable note", got)
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

package utils

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func compressGzip(t *testing.T, data string) string {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func compressZstd(t *testing.T, data string) string {
	t.Helper()
	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestDecompressGzip(t *testing.T) {
	original := "Section 42 of the Children and Families Act 2014 imposes an absolute duty on the local authority to secure the provision specified in Section F."
	encoded := compressGzip(t, original)

	result, err := decompress(encoded, "gzip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != original {
		t.Errorf("got %q, want %q", result, original)
	}
}

func TestDecompressZstd(t *testing.T) {
	original := "Section 42 of the Children and Families Act 2014 imposes an absolute duty on the local authority to secure the provision specified in Section F."
	encoded := compressZstd(t, original)

	result, err := decompress(encoded, "zstd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != original {
		t.Errorf("got %q, want %q", result, original)
	}
}

func TestDecompressUnsupportedAlgorithm(t *testing.T) {
	_, err := decompress(base64.StdEncoding.EncodeToString([]byte("test")), "lz4")
	if err == nil {
		t.Fatal("expected error for unsupported algorithm")
	}
}

func TestDecompressInvalidBase64(t *testing.T) {
	_, err := decompress("not-valid-base64!!!", "gzip")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDecompressBase64WithWhitespace(t *testing.T) {
	original := "Whitespace in base64 is common when users paste wrapped output."
	clean := compressGzip(t, original)

	// Insert newlines and spaces to simulate wrapped base64
	wrapped := clean[:20] + "\n" + clean[20:40] + "  " + clean[40:60] + "\r\n" + clean[60:]

	result, err := decompress(wrapped, "gzip")
	if err != nil {
		t.Fatalf("unexpected error with whitespace in base64: %v", err)
	}
	if result != original {
		t.Errorf("got %q, want %q", result, original)
	}
}

func TestDecompressExceedsSizeLimit(t *testing.T) {
	// Create data larger than maxDecompressedSize (10MB)
	// zstd compresses repeated data extremely well, so a small input can exceed the limit
	huge := make([]byte, maxDecompressedSize+1)
	for i := range huge {
		huge[i] = 'A'
	}

	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(huge); err != nil {
		t.Fatal(err)
	}
	w.Close()
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	_, err = decompress(encoded, "zstd")
	if err == nil {
		t.Fatal("expected error for oversized decompressed output")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected 'exceeds' in error message, got: %v", err)
	}
}

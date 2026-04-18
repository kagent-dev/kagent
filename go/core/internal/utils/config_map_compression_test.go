package utils

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
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

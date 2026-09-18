package tracing

import (
	"strings"
	"testing"
	"unicode/utf8"
	"unsafe"
)

func TestTextCaptureDisabled(t *testing.T) {
	capture := NewTextCapture(0)
	if capture != nil {
		t.Fatalf("NewTextCapture(0) = %v, want nil", capture)
	}
	capture.Append("dropped")
	if capture.Text() != "" || capture.Truncated() {
		t.Fatalf("disabled capture retained %q (truncated = %v)", capture.Text(), capture.Truncated())
	}
}

func TestTextCaptureKeepsThePrefix(t *testing.T) {
	capture := NewTextCapture(8)
	capture.Append("abc")
	capture.Append("de")
	if capture.Text() != "abcde" || capture.Truncated() {
		t.Fatalf("capture = %q (truncated = %v), want %q", capture.Text(), capture.Truncated(), "abcde")
	}
}

func TestTextCaptureTruncatesOnARuneBoundary(t *testing.T) {
	// Each character is three bytes, so a nine-byte budget cannot hold the
	// fourth one and must not keep half of it either.
	capture := NewTextCapture(9)
	capture.Append(strings.Repeat("ü", 1))
	capture.Append(strings.Repeat("日", 4))
	text := capture.Text()
	if !capture.Truncated() {
		t.Fatal("capture did not report truncation")
	}
	if !utf8.ValidString(text) {
		t.Fatalf("capture = %q, want valid UTF-8", text)
	}
	if len(text) > 9 {
		t.Fatalf("capture retained %d bytes, want at most 9", len(text))
	}
}

func TestTextCaptureBoundsMemoryAcrossManyDeltas(t *testing.T) {
	capture := NewTextCapture(64)
	for range 10_000 {
		capture.Append("0123456789")
	}
	if got := len(capture.Text()); got != 64 {
		t.Fatalf("capture retained %d bytes, want the 64-byte budget", got)
	}
	if !capture.Truncated() {
		t.Fatal("capture did not report truncation")
	}
}

func TestBoundedText(t *testing.T) {
	for _, test := range []struct {
		name          string
		text          string
		limit         int
		want          string
		wantTruncated bool
	}{
		{name: "within limit", text: "hello", limit: 16, want: "hello"},
		{name: "exact limit", text: "hello", limit: 5, want: "hello"},
		{name: "truncated", text: "hello", limit: 3, want: "hel", wantTruncated: true},
		{name: "multibyte", text: "日本", limit: 4, want: "日", wantTruncated: true},
		{name: "disabled with text", text: "hello", limit: 0, wantTruncated: true},
		{name: "disabled without text", limit: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			text, truncated := BoundedText(test.text, test.limit)
			if text != test.want || truncated != test.wantTruncated {
				t.Fatalf("BoundedText() = %q, %v, want %q, %v", text, truncated, test.want, test.wantTruncated)
			}
		})
	}
}

// A budget with room left after a rune did not fit must not be filled by a
// later, shorter delta, which would splice text that was never adjacent.
func TestTextCaptureStopsAtTheFirstTruncation(t *testing.T) {
	capture := NewTextCapture(4)
	capture.Append("日")
	capture.Append("本")
	capture.Append("x")
	if got := capture.Text(); got != "日" {
		t.Fatalf("capture = %q, want the bounded prefix %q", got, "日")
	}
	if !capture.Truncated() {
		t.Fatal("capture did not report truncation")
	}
}

// A shortened prefix must not keep the whole input alive, or the budget would
// bound the exported value rather than the memory capture costs.
func TestBoundedTextDoesNotRetainTheInput(t *testing.T) {
	text := strings.Repeat("p", 1<<20)
	bounded, truncated := BoundedText(text, 16)
	if !truncated || len(bounded) != 16 {
		t.Fatalf("BoundedText() = %d bytes, truncated = %v", len(bounded), truncated)
	}
	if unsafe.StringData(bounded) == unsafe.StringData(text) {
		t.Fatal("bounded text shares the input's backing array")
	}
}

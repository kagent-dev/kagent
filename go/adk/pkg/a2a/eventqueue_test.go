package a2a

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
)

// ---------------------------------------------------------------------------
// Part filter tests — functions live in eventqueue.go
// ---------------------------------------------------------------------------

func TestFilterTextParts(t *testing.T) {
	parts := a2atype.ContentParts{
		a2atype.TextPart{Text: "hello"},
		a2atype.DataPart{Data: map[string]any{"x": 1}},
		a2atype.TextPart{Text: "world"},
	}

	textOnly := filterTextParts(parts)
	if len(textOnly) != 2 {
		t.Fatalf("expected 2 text parts, got %d", len(textOnly))
	}
	if tp, ok := textOnly[0].(a2atype.TextPart); !ok || tp.Text != "hello" {
		t.Errorf("textOnly[0] = %v, want TextPart{hello}", textOnly[0])
	}
	if tp, ok := textOnly[1].(a2atype.TextPart); !ok || tp.Text != "world" {
		t.Errorf("textOnly[1] = %v, want TextPart{world}", textOnly[1])
	}
}

func TestIsEmptyDataPart(t *testing.T) {
	tests := []struct {
		name string
		part a2atype.Part
		want bool
	}{
		{"nil data DataPart", a2atype.DataPart{Data: nil}, true},
		{"empty data DataPart", a2atype.DataPart{Data: map[string]any{}}, true},
		{"non-empty DataPart", a2atype.DataPart{Data: map[string]any{"k": "v"}}, false},
		{"TextPart", a2atype.TextPart{Text: "hi"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEmptyDataPart(tt.part); got != tt.want {
				t.Errorf("isEmptyDataPart() = %v, want %v", got, tt.want)
			}
		})
	}
}

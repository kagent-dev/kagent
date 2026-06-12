package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/artifact"
	"google.golang.org/genai"
)

// fakeArtifacts is a minimal agent.Artifacts implementation that records saves.
type fakeArtifacts struct {
	saved    map[string]*genai.Part
	versions map[string]int64
	saveErr  error
}

func newFakeArtifacts() *fakeArtifacts {
	return &fakeArtifacts{saved: map[string]*genai.Part{}, versions: map[string]int64{}}
}

func (f *fakeArtifacts) Save(_ context.Context, name string, data *genai.Part) (*artifact.SaveResponse, error) {
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	f.versions[name]++
	f.saved[name] = data
	return &artifact.SaveResponse{Version: f.versions[name]}, nil
}

func (f *fakeArtifacts) List(context.Context) (*artifact.ListResponse, error) {
	names := make([]string, 0, len(f.saved))
	for n := range f.saved {
		names = append(names, n)
	}
	return &artifact.ListResponse{FileNames: names}, nil
}

func (f *fakeArtifacts) Load(_ context.Context, name string) (*artifact.LoadResponse, error) {
	return &artifact.LoadResponse{Part: f.saved[name]}, nil
}

func (f *fakeArtifacts) LoadVersion(ctx context.Context, name string, _ int) (*artifact.LoadResponse, error) {
	return f.Load(ctx, name)
}

func TestSaveArtifact(t *testing.T) {
	tests := []struct {
		name        string
		artifacts   agent.Artifacts
		input       saveArtifactInput
		limit       int
		wantErr     bool
		wantBytes   []byte
		wantMime    string
		wantVersion int64
	}{
		{
			name:        "text content stored as inline data",
			artifacts:   newFakeArtifacts(),
			input:       saveArtifactInput{Name: "note.txt", Content: "hello", MimeType: "text/plain"},
			limit:       1024,
			wantBytes:   []byte("hello"),
			wantMime:    "text/plain",
			wantVersion: 1,
		},
		{
			name:        "missing mime defaults to text/plain",
			artifacts:   newFakeArtifacts(),
			input:       saveArtifactInput{Name: "a.csv", Content: "a,b\n1,2"},
			limit:       1024,
			wantBytes:   []byte("a,b\n1,2"),
			wantMime:    "text/plain",
			wantVersion: 1,
		},
		{
			name:        "base64 content decoded",
			artifacts:   newFakeArtifacts(),
			input:       saveArtifactInput{Name: "img.bin", Content: base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03}), MimeType: "application/octet-stream", Base64: true},
			limit:       1024,
			wantBytes:   []byte{0x01, 0x02, 0x03},
			wantMime:    "application/octet-stream",
			wantVersion: 1,
		},
		{
			name:      "empty name rejected",
			artifacts: newFakeArtifacts(),
			input:     saveArtifactInput{Name: "  ", Content: "x"},
			limit:     1024,
			wantErr:   true,
		},
		{
			name:      "path separator rejected",
			artifacts: newFakeArtifacts(),
			input:     saveArtifactInput{Name: "dir/note.txt", Content: "x"},
			limit:     1024,
			wantErr:   true,
		},
		{
			name:      "invalid base64 rejected",
			artifacts: newFakeArtifacts(),
			input:     saveArtifactInput{Name: "x.bin", Content: "not base64!!!", Base64: true},
			limit:     1024,
			wantErr:   true,
		},
		{
			name:      "oversized content rejected",
			artifacts: newFakeArtifacts(),
			input:     saveArtifactInput{Name: "big.txt", Content: "0123456789"},
			limit:     5,
			wantErr:   true,
		},
		{
			name:      "nil artifact service rejected",
			artifacts: nil,
			input:     saveArtifactInput{Name: "x.txt", Content: "x"},
			limit:     1024,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := saveArtifact(context.Background(), tt.artifacts, tt.input, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("saveArtifact() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if got["version"] != tt.wantVersion {
				t.Errorf("version = %v, want %d", got["version"], tt.wantVersion)
			}
			if got["mime_type"] != tt.wantMime {
				t.Errorf("mime_type = %v, want %q", got["mime_type"], tt.wantMime)
			}

			fa := tt.artifacts.(*fakeArtifacts)
			part := fa.saved[tt.input.Name]
			if part == nil || part.InlineData == nil {
				t.Fatalf("expected artifact %q saved as inline data", tt.input.Name)
			}
			if string(part.InlineData.Data) != string(tt.wantBytes) {
				t.Errorf("stored bytes = %v, want %v", part.InlineData.Data, tt.wantBytes)
			}
			if part.InlineData.DisplayName != tt.input.Name {
				t.Errorf("display name = %q, want %q", part.InlineData.DisplayName, tt.input.Name)
			}
		})
	}
}

func TestSaveArtifact_PropagatesSaveError(t *testing.T) {
	fa := newFakeArtifacts()
	fa.saveErr = fmt.Errorf("store unavailable")
	_, err := saveArtifact(context.Background(), fa, saveArtifactInput{Name: "x.txt", Content: "x"}, 1024)
	if err == nil {
		t.Fatal("expected error when underlying Save fails")
	}
}

func TestNewSaveArtifactTool(t *testing.T) {
	tl, err := NewSaveArtifactTool()
	if err != nil {
		t.Fatalf("NewSaveArtifactTool() error = %v", err)
	}
	if tl.Name() != "save_artifact" {
		t.Errorf("tool name = %q, want %q", tl.Name(), "save_artifact")
	}
}

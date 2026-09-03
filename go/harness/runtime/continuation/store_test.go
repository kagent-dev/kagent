package continuation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStorePreservesStateFormatAndUsesRuntimeValidator(t *testing.T) {
	directory := t.TempDir()
	validate := func(id string) error {
		if id != "opaque-thread" {
			return errors.New("invalid ID")
		}
		return nil
	}
	store, err := New(directory, "codex", validate)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Bind("opaque-thread"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(directory, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"version\": 2,\n  \"runtime\": \"codex\",\n  \"session_id\": \"opaque-thread\"\n}"
	if string(contents) != want {
		t.Fatalf("state = %s, want %s", contents, want)
	}
	reloaded, err := New(directory, "codex", validate)
	if err != nil {
		t.Fatal(err)
	}
	if id, ok, err := reloaded.Load(); err != nil || !ok || id != "opaque-thread" {
		t.Fatalf("Load() = %q, %t, %v", id, ok, err)
	}
	if err := reloaded.Bind("different"); err == nil {
		t.Fatal("Bind() accepted an invalid continuation")
	}
}

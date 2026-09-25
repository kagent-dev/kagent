package continuation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestStoreLoadsStateRestoredAfterConstruction(t *testing.T) {
	directory := t.TempDir()
	validate := func(id string) error {
		if id == "" {
			return errors.New("empty ID")
		}
		return nil
	}
	store, err := New(directory, "codex", validate)
	if err != nil {
		t.Fatal(err)
	}

	// DATA_ON_GOLDEN resumes the process containing store, then restores this
	// file from the Actor's durable data snapshot.
	path := filepath.Join(directory, "state.json")
	restored := []byte("{\n  \"version\": 2,\n  \"runtime\": \"codex\",\n  \"session_id\": \"restored-thread\"\n}")
	if err := os.WriteFile(path, restored, 0o600); err != nil {
		t.Fatal(err)
	}

	if id, ok, err := store.Load(); err != nil || !ok || id != "restored-thread" {
		t.Fatalf("Load() = %q, %t, %v, want restored-thread, true, nil", id, ok, err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != string(restored) {
		t.Fatalf("state changed after Load():\n%s", contents)
	}
}

func TestStoreBindRejectsStateRestoredAfterConstruction(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory, "codex", func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "state.json")
	restored := []byte("{\n  \"version\": 2,\n  \"runtime\": \"codex\",\n  \"session_id\": \"restored-thread\"\n}")
	if err := os.WriteFile(path, restored, 0o600); err != nil {
		t.Fatal(err)
	}

	// Bind must independently reload the restored file. Call it without Load so
	// a stale in-memory implementation cannot pass by refreshing its cache first.
	if err := store.Bind("new-thread"); err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("Bind() error = %v, want already-bound error", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != string(restored) {
		t.Fatalf("state changed after rejected Bind():\n%s", contents)
	}
}

func TestStoreRejectsStateCorruptionRestoredAfterConstruction(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory, "claude", func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "state.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "decode continuation state") {
		t.Fatalf("Load() error = %v, want decode error", err)
	}
	if err := store.Bind("new-session"); err == nil || !strings.Contains(err.Error(), "decode continuation state") {
		t.Fatalf("Bind() error = %v, want decode error", err)
	}
}

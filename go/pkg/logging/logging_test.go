package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestNew(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, "warn")
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("hidden")
	logger.Warn("visible", "task_id", "task-1")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["level"] != "WARN" || record["msg"] != "visible" || record["task_id"] != "task-1" {
		t.Fatalf("unexpected record: %#v", record)
	}
}

func TestContextBridge(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil)).With("component", "test")
	FromContext(IntoContext(context.Background(), logger)).Info("bridged")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["component"] != "test" {
		t.Fatalf("context fields were lost: %#v", record)
	}
}

func TestNewRejectsInvalidLevel(t *testing.T) {
	if _, err := New(&bytes.Buffer{}, "verbose"); err == nil {
		t.Fatal("New accepted invalid level")
	}
}

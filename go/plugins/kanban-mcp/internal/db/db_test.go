package db_test

import (
	"testing"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
)

func TestValidStatus(t *testing.T) {
	tests := []struct {
		name   string
		status db.TaskStatus
		want   bool
	}{
		{"Inbox valid", db.StatusInbox, true},
		{"Design valid", db.StatusDesign, true},
		{"Develop valid", db.StatusDevelop, true},
		{"Testing valid", db.StatusTesting, true},
		{"SecurityScan valid", db.StatusSecurityScan, true},
		{"CodeReview valid", db.StatusCodeReview, true},
		{"Documentation valid", db.StatusDocumentation, true},
		{"Done valid", db.StatusDone, true},
		{"empty invalid", db.TaskStatus(""), false},
		{"unknown invalid", db.TaskStatus("invalid"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := db.ValidStatus(tt.status); got != tt.want {
				t.Errorf("ValidStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestNewManager_Sqlite(t *testing.T) {
	cfg := &config.Config{
		DBType: config.DBTypeSQLite,
		DBPath: "file::memory:?cache=shared",
	}
	mgr, err := db.NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if err := mgr.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !mgr.DB().Migrator().HasTable(&db.Task{}) {
		t.Error("tasks table does not exist after AutoMigrate")
	}
	if !mgr.DB().Migrator().HasTable(&db.Attachment{}) {
		t.Error("attachments table does not exist after AutoMigrate")
	}
}

func TestNewManager_InvalidType(t *testing.T) {
	cfg := &config.Config{
		DBType: config.DBType("invalid"),
	}
	_, err := db.NewManager(cfg)
	if err == nil {
		t.Error("NewManager() expected error for invalid DBType, got nil")
	}
}

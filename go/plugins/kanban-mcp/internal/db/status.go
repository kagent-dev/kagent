// Package db provides Postgres connection helpers and pure status/type logic
// for the kanban-mcp server. It is the local analogue of go/core/internal/database
// (which cannot be imported across the internal/ boundary).
package db

import (
	"path/filepath"
	"slices"
	"strings"
)

// TaskStatus is one of the fixed kanban workflow columns.
type TaskStatus string

const (
	StatusInbox      TaskStatus = "Inbox"
	StatusPlan       TaskStatus = "Plan"
	StatusDevelop    TaskStatus = "Develop"
	StatusTesting    TaskStatus = "Testing"
	StatusCodeReview TaskStatus = "CodeReview"
	StatusRelease    TaskStatus = "Release"
	StatusDone       TaskStatus = "Done"
)

// StatusWorkflow lists the statuses in their canonical board order. The board UI
// and the move-next/move-prev semantics rely on this ordering.
var StatusWorkflow = []TaskStatus{
	StatusInbox,
	StatusPlan,
	StatusDevelop,
	StatusTesting,
	StatusCodeReview,
	StatusRelease,
	StatusDone,
}

// ValidStatus reports whether s is one of the known workflow statuses.
func ValidStatus(s TaskStatus) bool {
	return slices.Contains(StatusWorkflow, s)
}

// DefaultColumns is the ordered column set seeded for the built-in "default"
// board. It mirrors StatusWorkflow as plain strings so it can be used directly as
// a board's columns (boards store columns as TEXT[] / []string, not TaskStatus).
var DefaultColumns = func() []string {
	cols := make([]string, len(StatusWorkflow))
	for i, s := range StatusWorkflow {
		cols[i] = string(s)
	}
	return cols
}()

// Board scope values. Boards are either shared ("general") or bound to a named
// agent ("agent") for UI grouping / convention. There is no access control.
const (
	BoardScopeGeneral = "general"
	BoardScopeAgent   = "agent"
)

// DefaultBoardKey is the key of the built-in board created by migration 000002,
// used as the fallback board when a caller omits an explicit board key.
const DefaultBoardKey = "default"

// ValidColumn reports whether s is one of the given board columns.
func ValidColumn(columns []string, s string) bool {
	return slices.Contains(columns, s)
}

// ValidScope reports whether s is a known board scope.
func ValidScope(s string) bool {
	return s == BoardScopeGeneral || s == BoardScopeAgent
}

// Task kinds, persisted in the task.kind column. A Feature is a top-level card;
// a Task is either a child of a Feature (parent_id set) or a standalone top-level
// card (parent_id NULL). Both are full kanban cards; only the Task level can
// carry checklist subtasks.
const (
	KindFeature = "feature"
	KindTask    = "task"
)

// AttachmentType is the kind of row stored in kanban.attachment. The table backs
// three shapes distinguished by this column: file (filename + base64 content),
// link (url + title), and attribute (a key/value pair: title=key, content=value).
type AttachmentType string

const (
	AttachmentTypeFile      AttachmentType = "file"
	AttachmentTypeLink      AttachmentType = "link"
	AttachmentTypeAttribute AttachmentType = "attribute"
)

// ValidUserAttachmentType reports whether t is a type accepted by add_attachment.
// Attributes are excluded: they are managed via set_attribute / delete_attribute.
func ValidUserAttachmentType(t AttachmentType) bool {
	return t == AttachmentTypeFile || t == AttachmentTypeLink
}

// AllowedFileExtensions is the set of filename extensions (lower-case, with the
// leading dot) accepted for file attachments. Text types render inline in the UI;
// binary types (pdf/docx/xlsx) are offered as downloads.
var AllowedFileExtensions = map[string]bool{
	".md":       true,
	".markdown": true,
	".html":     true,
	".htm":      true,
	".txt":      true,
	".yaml":     true,
	".yml":      true,
	".csv":      true,
	".pdf":      true,
	".docx":     true,
	".xlsx":     true,
}

// ValidFileExtension reports whether filename ends in an allowed extension
// (case-insensitive).
func ValidFileExtension(filename string) bool {
	return AllowedFileExtensions[strings.ToLower(filepath.Ext(filename))]
}

// AllowedFileExtensionList returns the allowed extensions as a sorted,
// comma-separated string for use in error messages.
func AllowedFileExtensionList() string {
	exts := make([]string, 0, len(AllowedFileExtensions))
	for e := range AllowedFileExtensions {
		exts = append(exts, e)
	}
	slices.Sort(exts)
	return strings.Join(exts, ", ")
}

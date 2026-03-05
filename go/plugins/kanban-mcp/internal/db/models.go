package db

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// TaskStatus represents the workflow state of a task.
type TaskStatus string

const (
	StatusInbox         TaskStatus = "Inbox"
	StatusDesign        TaskStatus = "Design"
	StatusDevelop       TaskStatus = "Develop"
	StatusTesting       TaskStatus = "Testing"
	StatusSecurityScan  TaskStatus = "SecurityScan"
	StatusCodeReview    TaskStatus = "CodeReview"
	StatusDocumentation TaskStatus = "Documentation"
	StatusDone          TaskStatus = "Done"
)

// StatusWorkflow defines the ordered workflow for tasks.
var StatusWorkflow = []TaskStatus{
	StatusInbox,
	StatusDesign,
	StatusDevelop,
	StatusTesting,
	StatusSecurityScan,
	StatusCodeReview,
	StatusDocumentation,
	StatusDone,
}

// ValidStatus returns true if s is one of the 8 workflow statuses.
func ValidStatus(s TaskStatus) bool {
	for _, v := range StatusWorkflow {
		if v == s {
			return true
		}
	}
	return false
}

// StringSlice is a custom type for storing string slices as JSON in the database.
type StringSlice []string

// Scan implements the sql.Scanner interface for StringSlice.
func (s *StringSlice) Scan(value interface{}) error {
	if value == nil {
		*s = nil
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		*s = nil
		return nil
	}
	if len(bytes) == 0 || string(bytes) == "null" {
		*s = nil
		return nil
	}
	return json.Unmarshal(bytes, s)
}

// Value implements the driver.Valuer interface for StringSlice.
func (s StringSlice) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

// Task is the GORM model for a kanban task.
type Task struct {
	ID              uint   `gorm:"primarykey"`
	Title           string `gorm:"not null"`
	Description     string
	Status          TaskStatus `gorm:"not null;default:'Inbox'"`
	Assignee        string
	Labels          StringSlice `gorm:"type:text"`
	UserInputNeeded bool        `gorm:"not null;default:false"`
	ParentID        *uint
	Subtasks        []*Task       `gorm:"foreignKey:ParentID"`
	Attachments     []*Attachment `gorm:"foreignKey:TaskID"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// AttachmentType represents the type of attachment.
type AttachmentType string

const (
	AttachmentTypeFile AttachmentType = "file"
	AttachmentTypeLink AttachmentType = "link"
)

// Attachment is the GORM model for a task attachment.
type Attachment struct {
	ID        uint           `gorm:"primarykey"`
	TaskID    uint           `gorm:"not null;index"`
	Type      AttachmentType `gorm:"type:varchar(16);not null"`
	Filename  string         `gorm:"type:varchar(255)"`
	Content   string         `gorm:"type:text"`
	URL       string         `gorm:"type:text"`
	Title     string         `gorm:"type:varchar(255)"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

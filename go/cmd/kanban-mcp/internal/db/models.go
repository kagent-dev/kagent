package db

import "time"

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

// Task is the GORM model for a kanban task.
type Task struct {
	ID              uint       `gorm:"primarykey"`
	Title           string     `gorm:"not null"`
	Description     string
	Status          TaskStatus `gorm:"not null;default:'Inbox'"`
	Assignee        string
	UserInputNeeded bool    `gorm:"not null;default:false"`
	ParentID        *uint
	Subtasks        []*Task `gorm:"foreignKey:ParentID"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

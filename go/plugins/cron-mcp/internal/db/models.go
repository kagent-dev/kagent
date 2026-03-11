package db

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// JobStatus represents the current state of a cron job.
type JobStatus string

const (
	StatusActive   JobStatus = "Active"
	StatusPaused   JobStatus = "Paused"
	StatusError    JobStatus = "Error"
	StatusArchived JobStatus = "Archived"
)

// StatusList defines all valid job statuses.
var StatusList = []JobStatus{
	StatusActive,
	StatusPaused,
	StatusError,
	StatusArchived,
}

// ValidStatus returns true if s is a valid job status.
func ValidStatus(s JobStatus) bool {
	for _, v := range StatusList {
		if v == s {
			return true
		}
	}
	return false
}

// ExecStatus represents the status of a single execution.
type ExecStatus string

const (
	ExecRunning ExecStatus = "Running"
	ExecSuccess ExecStatus = "Success"
	ExecFailed  ExecStatus = "Failed"
)

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

// CronJob is the GORM model for a cron job definition.
type CronJob struct {
	ID            uint   `gorm:"primarykey"`
	Name          string `gorm:"not null"`
	Description   string
	Schedule      string      `gorm:"not null"` // cron expression e.g. "*/5 * * * *"
	Command       string      `gorm:"not null;type:text"`
	Status        JobStatus   `gorm:"not null;default:'Active'"`
	Labels        StringSlice `gorm:"type:text"`
	Timeout       int         `gorm:"not null;default:300"` // seconds
	MaxRetries    int         `gorm:"not null;default:0"`
	LastRunAt     *time.Time
	LastRunStatus *ExecStatus
	NextRunAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Executions    []*Execution `gorm:"foreignKey:CronJobID"`
}

// Execution is the GORM model for a single cron job execution.
type Execution struct {
	ID         uint       `gorm:"primarykey"`
	CronJobID  uint       `gorm:"not null;index"`
	Status     ExecStatus `gorm:"not null;default:'Running'"`
	Output     string     `gorm:"type:text"`
	ExitCode   *int
	StartedAt  time.Time `gorm:"not null"`
	FinishedAt *time.Time
	Duration   *float64 // seconds
	CreatedAt  time.Time
}

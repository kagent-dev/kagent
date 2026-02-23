// Package database: time types that scan from both time.Time and string.
// Turso (libSQL) returns datetime columns as TEXT, so scanning into time.Time
// fails. These types accept string or time.Time so GORM works with Turso.

package database

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// FlexibleTime is a time.Time that implements sql.Scanner and driver.Valuer
// and accepts string or []byte from the DB (e.g. Turso) as well as time.Time.
// Use for CreatedAt, UpdatedAt, DeletedAt and any non-nullable time column.
type FlexibleTime struct {
	time.Time
}

// Scan implements sql.Scanner so Turso's TEXT datetime columns are parsed.
func (t *FlexibleTime) Scan(value interface{}) error {
	if value == nil {
		t.Time = time.Time{}
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		t.Time = v
		return nil
	case []byte:
		return t.parse(string(v))
	case string:
		return t.parse(v)
	default:
		return fmt.Errorf("FlexibleTime: unsupported scan type %T", value)
	}
}

// Value implements driver.Valuer for inserts/updates.
func (t FlexibleTime) Value() (driver.Value, error) {
	return t.Time, nil
}

func (t *FlexibleTime) parse(s string) error {
	if s == "" {
		t.Time = time.Time{}
		return nil
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if parsed, err := time.Parse(f, s); err == nil {
			t.Time = parsed
			return nil
		}
	}
	return fmt.Errorf("FlexibleTime: failed to parse %q", s)
}

// NullableFlexibleTime is for optional time columns (e.g. ExpiresAt, LastConnected).
// Valid is false when the DB value is NULL.
type NullableFlexibleTime struct {
	Time  FlexibleTime
	Valid bool
}

// Scan implements sql.Scanner.
func (n *NullableFlexibleTime) Scan(value interface{}) error {
	if value == nil {
		n.Valid = false
		n.Time = FlexibleTime{}
		return nil
	}
	n.Valid = true
	return n.Time.Scan(value)
}

// Value implements driver.Valuer.
func (n NullableFlexibleTime) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Time.Value()
}

// Ptr returns *time.Time for API compatibility when the value is valid.
func (n NullableFlexibleTime) Ptr() *time.Time {
	if !n.Valid {
		return nil
	}
	return &n.Time.Time
}

// FromTime creates a NullableFlexibleTime from a *time.Time.
func FromTime(t *time.Time) NullableFlexibleTime {
	if t == nil {
		return NullableFlexibleTime{Valid: false}
	}
	return NullableFlexibleTime{Time: FlexibleTime{Time: *t}, Valid: true}
}

// Package scheduling contains the scheduling semantics shared by services and
// transactional reservation. Adapted from ScheduledRun in kagent-dev/kagent#2097.
package scheduling

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // Scheduling must work in images without system timezone data.

	"github.com/robfig/cron/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// Normalize supplies defaults without mutating a caller's request.
func Normalize(spec v1alpha3.ScheduledRunSpec) v1alpha3.ScheduledRunSpec {
	result := spec.DeepCopy()
	result.Schedule = strings.Join(strings.Fields(result.Schedule), " ")
	if result.TimeZone == nil {
		result.TimeZone = new("UTC")
	}
	if result.Paused == nil {
		result.Paused = new(false)
	}
	if result.ExecutionTimeout == nil {
		result.ExecutionTimeout = &metav1.Duration{Duration: 15 * time.Minute}
	}
	return *result
}

// Validate checks a CRD spec at service and persistence boundaries, including
// the cron and time-zone semantics that Kubernetes admission cannot evaluate.
func Validate(spec v1alpha3.ScheduledRunSpec) error {
	if len(validation.IsDNS1123Subdomain(spec.AgentRef.Name)) != 0 {
		return fmt.Errorf("agentRef.name must be a valid resource name")
	}
	if strings.TrimSpace(spec.Prompt) == "" || len(spec.Prompt) > 32768 {
		return fmt.Errorf("prompt must contain between 1 and 32768 bytes")
	}
	if len(spec.Schedule) == 0 || len(spec.Schedule) > 256 {
		return fmt.Errorf("schedule must contain between 1 and 256 characters")
	}
	if zone := ptr.Deref(spec.TimeZone, "UTC"); len(zone) == 0 || len(zone) > 253 {
		return fmt.Errorf("timeZone must contain between 1 and 253 characters")
	}
	if spec.ExecutionTimeout != nil && (spec.ExecutionTimeout.Duration < time.Microsecond || spec.ExecutionTimeout.Duration > 2562047*time.Hour) {
		return fmt.Errorf("executionTimeout must be between one microsecond and 2562047 hours")
	}
	_, err := Next(spec, time.Now())
	return err
}

// Next requires five cron fields, even though ParseStandard also accepts macros.
// All callers, including transactions, use the same timezone and DST semantics.
func Next(spec v1alpha3.ScheduledRunSpec, after time.Time) (time.Time, error) {
	zone := ptr.Deref(spec.TimeZone, "UTC")
	if len(strings.Fields(spec.Schedule)) != 5 {
		return time.Time{}, fmt.Errorf("schedule must contain five cron fields")
	}
	if zone == "Local" {
		return time.Time{}, fmt.Errorf("time zone must not depend on the scheduler host")
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return time.Time{}, fmt.Errorf("invalid time zone: %w", err)
	}
	schedule, err := cron.ParseStandard("CRON_TZ=" + zone + " " + spec.Schedule)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid schedule: %w", err)
	}
	next := schedule.Next(after)
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("schedule has no next occurrence")
	}
	return next.UTC(), nil
}

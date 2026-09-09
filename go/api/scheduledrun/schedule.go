// Package scheduledrun contains the scheduling semantics shared by services and
// transactional reservation. Adapted from ScheduledRun in kagent-dev/kagent#2097.
package scheduledrun

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // Scheduling must work in images without system timezone data.

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/robfig/cron/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Normalize supplies defaults without mutating a caller's request.
func Normalize(config *apiv1alpha1.ScheduledRunConfig) *apiv1alpha1.ScheduledRunConfig {
	result := proto.CloneOf(config)
	if result == nil {
		result = &apiv1alpha1.ScheduledRunConfig{}
	}
	result.Schedule = strings.Join(strings.Fields(result.Schedule), " ")
	if result.TimeZone == "" {
		result.TimeZone = "UTC"
	}
	if result.ExecutionTimeout == nil {
		result.ExecutionTimeout = durationpb.New(15 * time.Minute)
	}
	return result
}

// Next requires five cron fields, even though ParseStandard also accepts macros.
// All callers, including transactions, use the same timezone and DST semantics.
func Next(config *apiv1alpha1.ScheduledRunConfig, after time.Time) (time.Time, error) {
	if len(strings.Fields(config.GetSchedule())) != 5 {
		return time.Time{}, fmt.Errorf("schedule must contain five cron fields")
	}
	if config.GetTimeZone() == "Local" {
		return time.Time{}, fmt.Errorf("time zone must not depend on the scheduler host")
	}
	if _, err := time.LoadLocation(config.GetTimeZone()); err != nil {
		return time.Time{}, fmt.Errorf("invalid time zone: %w", err)
	}
	schedule, err := cron.ParseStandard("CRON_TZ=" + config.GetTimeZone() + " " + config.GetSchedule())
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid schedule: %w", err)
	}
	next := schedule.Next(after)
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("schedule has no next occurrence")
	}
	return next.UTC(), nil
}

// Package scheduledrun contains the scheduling semantics shared by services and
// transactional reservation. Adapted from ScheduledRun in kagent-dev/kagent#2097.
package scheduledrun

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // Scheduling must work in images without system timezone data.

	"buf.build/go/protovalidate"
	"github.com/robfig/cron/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// KubernetesCreator is the owner recorded for schedules declared by a
// ScheduledRun resource. It is namespaced so that control-plane rows cannot be
// read or edited through the creator-scoped gRPC API by a human principal.
func KubernetesCreator(namespace, name string) string {
	return "scheduledrun.api.kagent.dev/" + namespace + "/" + name
}

// Validate reports configuration the store rejects: protobuf field constraints
// and scheduling semantics. Callers outside the gRPC interceptor need it before
// persisting a normalized configuration.
func Validate(config *apiv1.ScheduledRunConfig) error {
	if err := protovalidate.Validate(config); err != nil {
		return err
	}
	_, err := Next(config, time.Now())
	return err
}

// RequestHash fingerprints the immutable creation inputs of a schedule for
// request deduplication. It hashes the normalized inputs, never the mutable
// persisted configuration.
func RequestHash(agent *apiv1.ResourceReference, config *apiv1.ScheduledRunConfig) ([]byte, error) {
	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&apiv1.CreateScheduledRunRequest{Agent: agent, Config: config})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	return sum[:], nil
}

// Normalize supplies defaults without mutating a caller's request.
func Normalize(config *apiv1.ScheduledRunConfig) *apiv1.ScheduledRunConfig {
	result := proto.CloneOf(config)
	if result == nil {
		result = &apiv1.ScheduledRunConfig{}
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
func Next(config *apiv1.ScheduledRunConfig, after time.Time) (time.Time, error) {
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

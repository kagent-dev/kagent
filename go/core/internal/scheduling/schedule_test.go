package scheduling_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/scheduling"
)

func TestNext(t *testing.T) {
	for _, tc := range []struct{ name, cron, zone, after, want string }{
		{"UTC", "0 9 * * *", "UTC", "2026-09-06T09:00:00Z", "2026-09-07T09:00:00Z"},
		{"local time", "0 9 * * *", "America/New_York", "2026-09-06T09:00:00Z", "2026-09-06T13:00:00Z"},
		{"spring gap", "30 2 * * *", "America/New_York", "2026-03-08T06:00:00Z", "2026-03-09T06:30:00Z"},
		{"fall fold", "30 1 * * *", "America/New_York", "2026-11-01T05:30:00Z", "2026-11-01T06:30:00Z"},
		{"host-dependent zone", "0 9 * * *", "Local", "2026-01-01T00:00:00Z", ""},
		{"macro", "@daily", "UTC", "2026-01-01T00:00:00Z", ""},
		{"seconds", "0 * * * * *", "UTC", "2026-01-01T00:00:00Z", ""},
		{"invalid field", "80 * * * *", "UTC", "2026-01-01T00:00:00Z", ""},
		{"impossible date", "0 0 31 2 *", "UTC", "2026-01-01T00:00:00Z", ""},
		{"unknown zone", "0 9 * * *", "Not/AZone", "2026-01-01T00:00:00Z", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after, err := time.Parse(time.RFC3339, tc.after)
			require.NoError(t, err)
			next, err := scheduling.Next(v1alpha3.ScheduledRunSpec{Schedule: tc.cron, TimeZone: new(tc.zone)}, after)
			if tc.want == "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, next.Format(time.RFC3339))
		})
	}
}

func TestNormalizePreservesRequest(t *testing.T) {
	request := v1alpha3.ScheduledRunSpec{Schedule: " 0  9 * * * ", Prompt: " keep whitespace "}
	config := scheduling.Normalize(request)
	require.Equal(t, "0 9 * * *", config.Schedule)
	require.Equal(t, "UTC", *config.TimeZone)
	require.Equal(t, 15*time.Minute, config.ExecutionTimeout.Duration)
	require.Equal(t, request.Prompt, config.Prompt)
	require.Empty(t, request.TimeZone)
	require.Nil(t, request.ExecutionTimeout)
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*v1alpha3.ScheduledRunSpec)
		wantErr bool
	}{
		{"defaults", func(*v1alpha3.ScheduledRunSpec) {}, false},
		{"minimum timeout", func(s *v1alpha3.ScheduledRunSpec) { s.ExecutionTimeout = &metav1.Duration{Duration: time.Second} }, false},
		{"maximum timeout", func(s *v1alpha3.ScheduledRunSpec) {
			s.ExecutionTimeout = &metav1.Duration{Duration: 2562047 * time.Hour}
		}, false},
		{"timeout below minimum", func(s *v1alpha3.ScheduledRunSpec) {
			s.ExecutionTimeout = &metav1.Duration{Duration: time.Second - time.Nanosecond}
		}, true},
		{"timeout over maximum", func(s *v1alpha3.ScheduledRunSpec) {
			s.ExecutionTimeout = &metav1.Duration{Duration: 2562047*time.Hour + time.Second}
		}, true},
		{"missing agent", func(s *v1alpha3.ScheduledRunSpec) { s.AgentRef.Name = "" }, true},
		{"invalid agent", func(s *v1alpha3.ScheduledRunSpec) { s.AgentRef.Name = "team/agent" }, true},
		{"blank prompt", func(s *v1alpha3.ScheduledRunSpec) { s.Prompt = " \n\t" }, true},
		{"oversized prompt", func(s *v1alpha3.ScheduledRunSpec) { s.Prompt = strings.Repeat("a", 32769) }, true},
		{"empty timezone", func(s *v1alpha3.ScheduledRunSpec) { s.TimeZone = new("") }, true},
		{"paused invalid cron", func(s *v1alpha3.ScheduledRunSpec) { s.Paused = new(true); s.Schedule = "@daily" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := v1alpha3.ScheduledRunSpec{AgentRef: corev1.LocalObjectReference{Name: "assistant"}, Schedule: "0 9 * * *", Prompt: "Report"}
			tc.change(&spec)
			err := scheduling.Validate(spec)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

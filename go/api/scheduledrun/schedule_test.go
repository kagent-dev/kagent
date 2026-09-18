package scheduledrun_test

import (
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/scheduledrun"
	"github.com/stretchr/testify/require"
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
			next, err := scheduledrun.Next(&apiv1alpha1.ScheduledRunConfig{Schedule: tc.cron, TimeZone: tc.zone}, after)
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
	request := &apiv1alpha1.ScheduledRunConfig{Schedule: " 0  9 * * * ", Prompt: " keep whitespace "}
	config := scheduledrun.Normalize(request)
	require.Equal(t, "0 9 * * *", config.Schedule)
	require.Equal(t, "UTC", config.TimeZone)
	require.Equal(t, 15*time.Minute, config.ExecutionTimeout.AsDuration())
	require.Equal(t, request.Prompt, config.Prompt)
	require.Empty(t, request.TimeZone)
	require.Nil(t, request.ExecutionTimeout)
}

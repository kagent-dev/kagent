package instance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAge(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	since := func(d time.Duration) *timestamppb.Timestamp { return timestamppb.New(now.Add(-d)) }

	tests := []struct {
		name    string
		created *timestamppb.Timestamp
		want    string
	}{
		{name: "absent renders empty, not an epoch age", created: nil, want: ""},
		{name: "seconds", created: since(30 * time.Second), want: "30s"},
		{name: "minutes", created: since(90 * time.Second), want: "1m"},
		{name: "hours", created: since(3 * time.Hour), want: "3h"},
		{name: "days", created: since(50 * time.Hour), want: "2d"},
		{name: "clock skew clamps to zero", created: since(-time.Minute), want: "0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Age(tt.created, now))
		})
	}
}

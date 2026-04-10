package a2a

import (
	"context"
	"fmt"
	"testing"
	"time"

	adkapi "github.com/kagent-dev/kagent/go/api/adk"
	"github.com/stretchr/testify/assert"
)

func TestComputeRetryDelay(t *testing.T) {
	tests := []struct {
		name    string
		attempt int
		policy  *adkapi.RetryPolicyConfig
		want    time.Duration
	}{
		{
			name:    "first attempt",
			attempt: 0,
			policy:  &adkapi.RetryPolicyConfig{MaxRetries: 3, InitialRetryDelay: 1.0},
			want:    1 * time.Second,
		},
		{
			name:    "second attempt doubles",
			attempt: 1,
			policy:  &adkapi.RetryPolicyConfig{MaxRetries: 3, InitialRetryDelay: 1.0},
			want:    2 * time.Second,
		},
		{
			name:    "third attempt quadruples",
			attempt: 2,
			policy:  &adkapi.RetryPolicyConfig{MaxRetries: 3, InitialRetryDelay: 1.0},
			want:    4 * time.Second,
		},
		{
			name:    "capped by max delay",
			attempt: 3,
			policy:  &adkapi.RetryPolicyConfig{MaxRetries: 5, InitialRetryDelay: 1.0, MaxRetryDelay: new(5.0)},
			want:    5 * time.Second,
		},
		{
			name:    "sub-second initial delay",
			attempt: 0,
			policy:  &adkapi.RetryPolicyConfig{MaxRetries: 3, InitialRetryDelay: 0.5},
			want:    500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeRetryDelay(tt.attempt, tt.policy)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExecuteWithRetry(t *testing.T) {
	tests := []struct {
		name      string
		policy    *adkapi.RetryPolicyConfig
		failCount int
		wantCalls int
		wantErr   bool
	}{
		{
			name:      "no retry policy, success",
			policy:    nil,
			failCount: 0,
			wantCalls: 1,
			wantErr:   false,
		},
		{
			name:      "no retry policy, failure",
			policy:    nil,
			failCount: 1,
			wantCalls: 1,
			wantErr:   true,
		},
		{
			name:      "retry policy, success on second attempt",
			policy:    &adkapi.RetryPolicyConfig{MaxRetries: 3, InitialRetryDelay: 0.001},
			failCount: 1,
			wantCalls: 2,
			wantErr:   false,
		},
		{
			name:      "retry policy, all attempts fail",
			policy:    &adkapi.RetryPolicyConfig{MaxRetries: 2, InitialRetryDelay: 0.001},
			failCount: 10,
			wantCalls: 3,
			wantErr:   true,
		},
		{
			name:      "context cancelled, no retry",
			policy:    &adkapi.RetryPolicyConfig{MaxRetries: 3, InitialRetryDelay: 0.001},
			failCount: -1, // special: returns context.Canceled
			wantCalls: 1,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			fn := func(ctx context.Context) error {
				callCount++
				if tt.failCount == -1 {
					return context.Canceled
				}
				if callCount <= tt.failCount {
					return fmt.Errorf("transient error %d", callCount)
				}
				return nil
			}

			ctx := context.Background()
			err := executeWithRetry(ctx, tt.policy, fn)

			assert.Equal(t, tt.wantCalls, callCount)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

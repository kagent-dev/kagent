package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func TestScheduledRunScheduler_UpdateSchedule(t *testing.T) {
	scheduler := NewScheduledRunScheduler(nil, nil)

	t.Run("adds entry for valid schedule", func(t *testing.T) {
		sr := &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sr",
				Namespace: "default",
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "0 */2 * * *",
				Prompt:   "hello",
			},
		}

		err := scheduler.UpdateSchedule(sr)
		require.NoError(t, err)

		key := types.NamespacedName{Name: "test-sr", Namespace: "default"}
		_, exists := scheduler.entries[key]
		assert.True(t, exists, "entry should be registered")
	})

	t.Run("removes entry when suspended", func(t *testing.T) {
		sr := &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "suspended-sr",
				Namespace: "default",
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "0 */2 * * *",
				Prompt:   "hello",
				Suspend:  false,
			},
		}

		// First add
		err := scheduler.UpdateSchedule(sr)
		require.NoError(t, err)

		key := types.NamespacedName{Name: "suspended-sr", Namespace: "default"}
		_, exists := scheduler.entries[key]
		assert.True(t, exists)

		// Now suspend
		sr.Spec.Suspend = true
		err = scheduler.UpdateSchedule(sr)
		require.NoError(t, err)

		_, exists = scheduler.entries[key]
		assert.False(t, exists, "entry should be removed when suspended")
	})

	t.Run("replaces entry on re-schedule", func(t *testing.T) {
		sr := &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "replace-sr",
				Namespace: "default",
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "0 */2 * * *",
				Prompt:   "hello",
			},
		}

		err := scheduler.UpdateSchedule(sr)
		require.NoError(t, err)

		key := types.NamespacedName{Name: "replace-sr", Namespace: "default"}
		firstID := scheduler.entries[key]

		// Update schedule
		sr.Spec.Schedule = "0 */3 * * *"
		err = scheduler.UpdateSchedule(sr)
		require.NoError(t, err)

		secondID := scheduler.entries[key]
		assert.NotEqual(t, firstID, secondID, "entry ID should change on re-schedule")
	})

	t.Run("returns error for invalid cron expression", func(t *testing.T) {
		sr := &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-sr",
				Namespace: "default",
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "invalid",
				Prompt:   "hello",
			},
		}

		err := scheduler.UpdateSchedule(sr)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to add cron schedule")
	})
}

func TestScheduledRunScheduler_RemoveSchedule(t *testing.T) {
	scheduler := NewScheduledRunScheduler(nil, nil)

	t.Run("removes existing entry", func(t *testing.T) {
		sr := &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "to-remove",
				Namespace: "default",
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "0 */2 * * *",
				Prompt:   "hello",
			},
		}

		err := scheduler.UpdateSchedule(sr)
		require.NoError(t, err)

		key := types.NamespacedName{Name: "to-remove", Namespace: "default"}
		_, exists := scheduler.entries[key]
		assert.True(t, exists)

		scheduler.RemoveSchedule(key)
		_, exists = scheduler.entries[key]
		assert.False(t, exists)
	})

	t.Run("no-op for non-existing entry", func(t *testing.T) {
		key := types.NamespacedName{Name: "nonexistent", Namespace: "default"}
		// Should not panic
		scheduler.RemoveSchedule(key)
	})
}

func TestScheduledRunScheduler_GetNextRunTime(t *testing.T) {
	scheduler := NewScheduledRunScheduler(nil, nil)

	t.Run("returns next time for valid cron", func(t *testing.T) {
		next, err := scheduler.GetNextRunTime("0 * * * *")
		require.NoError(t, err)
		require.NotNil(t, next)
		assert.True(t, next.After(time.Now()), "next run time should be in the future")
	})

	t.Run("returns error for invalid cron", func(t *testing.T) {
		_, err := scheduler.GetNextRunTime("not-a-cron")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse cron schedule")
	})
}

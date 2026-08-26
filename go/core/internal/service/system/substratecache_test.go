package system

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The cache exists to make a ~1.6s walk happen less often. These pin the two
// properties that make it safe to do that: a stale answer is never presented as a
// fresh one, and concurrent identical questions cost one walk rather than many.
func TestSubstrateCache(t *testing.T) {
	t.Run("recomputes once the entry has aged out", func(t *testing.T) {
		cache := newSubstrateCache()
		clock := time.Unix(1_800_000_000, 0)
		cache.now = func() time.Time { return clock }

		calls := 0
		compute := func() (any, error) {
			calls++
			return calls, nil
		}

		first, firstAt, err := cache.get(t.Context(), "k", compute)
		require.NoError(t, err)
		assert.Equal(t, 1, first)
		assert.Equal(t, clock, firstAt)

		// Inside the window: the same answer, and the same age — which is the point.
		// A cache that reported `now` here would make a stale number look live.
		clock = clock.Add(substrateCacheTTL / 2)
		second, secondAt, err := cache.get(t.Context(), "k", compute)
		require.NoError(t, err)
		assert.Equal(t, 1, second, "should have been served from the cache")
		assert.Equal(t, firstAt, secondAt, "a cached answer must report when it was computed")
		assert.Equal(t, 1, calls)

		// Past the window: computed again, and the age moves with it.
		clock = clock.Add(substrateCacheTTL)
		third, thirdAt, err := cache.get(t.Context(), "k", compute)
		require.NoError(t, err)
		assert.Equal(t, 2, third)
		assert.True(t, thirdAt.After(firstAt))
		assert.Equal(t, 2, calls)
	})

	t.Run("a different question is a different answer, not a stale one", func(t *testing.T) {
		cache := newSubstrateCache()
		calls := 0
		compute := func() (any, error) {
			calls++
			return calls, nil
		}

		_, _, err := cache.get(t.Context(), "actors|team||100||status|asc", compute)
		require.NoError(t, err)
		// Same read, different sort — a cache keyed too loosely would answer this
		// with the previous order's rows.
		_, _, err = cache.get(t.Context(), "actors|team||100||status|desc", compute)
		require.NoError(t, err)
		assert.Equal(t, 2, calls)
	})

	t.Run("concurrent identical requests share one walk", func(t *testing.T) {
		cache := newSubstrateCache()
		var mutex sync.Mutex
		calls := 0
		release := make(chan struct{})

		compute := func() (any, error) {
			mutex.Lock()
			calls++
			mutex.Unlock()
			<-release
			return "value", nil
		}

		const callers = 8
		var waiting sync.WaitGroup
		waiting.Add(callers)
		for range callers {
			go func() {
				defer waiting.Done()
				value, _, err := cache.get(context.Background(), "k", compute)
				assert.NoError(t, err)
				assert.Equal(t, "value", value)
			}()
		}

		// Let them all queue behind the one in flight, then finish it.
		time.Sleep(50 * time.Millisecond)
		close(release)
		waiting.Wait()

		mutex.Lock()
		defer mutex.Unlock()
		assert.Equal(t, 1, calls, "the walk should have happened once for all callers")
	})

	t.Run("a failure is not cached", func(t *testing.T) {
		cache := newSubstrateCache()
		calls := 0
		compute := func() (any, error) {
			calls++
			return nil, errors.New("ate-api is down")
		}

		_, _, err := cache.get(t.Context(), "k", compute)
		require.Error(t, err)
		_, _, err = cache.get(t.Context(), "k", compute)
		require.Error(t, err)
		// Caching the failure would keep a recovered backend looking broken for as
		// long as the entry lived.
		assert.Equal(t, 2, calls)
	})

	t.Run("holds a bounded number of entries", func(t *testing.T) {
		cache := newSubstrateCache()
		for index := range substrateCacheEntries * 3 {
			key := string(rune('a'+index%26)) + string(rune('0'+index/26))
			_, _, err := cache.get(t.Context(), key, func() (any, error) { return index, nil })
			require.NoError(t, err)
		}
		cache.mutex.Lock()
		defer cache.mutex.Unlock()
		assert.LessOrEqual(t, len(cache.entries), substrateCacheEntries)
	})
}

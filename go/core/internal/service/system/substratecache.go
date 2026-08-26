package system

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

/*
 * A short-lived cache in front of the substrate reads, and why it reports its own
 * age.
 *
 * # The cost this exists for
 *
 * Every one of the three substrate reads walks ate-api's whole actor list, because
 * ate-api offers no filter, no ordering and no server-side count — only pagination.
 * On a deployment holding 410,110 actors that measured at ~1.6s per call, and the
 * substrate page makes three of them on load and again on every poll tick.
 *
 * Asking ate-api for larger pages does not help: the page size was raised to its
 * maximum of 1000 and the timing did not move, so the cost is ate-api's own scan
 * rather than the number of round trips. There is nothing to optimise on this side
 * of that call; the only lever left is to make the same call less often.
 *
 * # Why the age is part of the answer
 *
 * Because the page above this offers polling, and a cache is exactly how polling
 * becomes a lie: the reader turns it on, the requests go out, the responses come
 * back instantly, and the numbers never change. This codebase has already shipped
 * that once — a poll control that reported it was re-reading and was not — and the
 * fix then was to measure rather than to trust.
 *
 * So every cached answer carries the instant it was computed. A caller can say "as
 * of 0.4s ago" instead of implying "now", and a reader watching a stalled cluster
 * can tell the difference between nothing changing and nothing being read. The TTL
 * is deliberately shorter than the page's default poll interval, so an ordinary
 * poll misses the cache and genuinely re-reads; what the cache absorbs is the burst
 * of identical requests a single page load makes.
 */

// substrateCacheTTL is how long a computed answer may be reused.
//
// Below the substrate page's default one-second poll and at its half-second floor,
// so polling at any offered rate still reaches ate-api. What this collapses is the
// three-or-more identical requests one page load makes — including React rendering
// a component twice in development.
const substrateCacheTTL = 400 * time.Millisecond

// substrateCacheEntries caps how many distinct answers are held.
//
// Each entry is one page of rows or one set of counts, so the cap bounds memory at
// something small and fixed. Distinct entries come from distinct questions — a
// different filter, sort or page — and a reader cannot generate many of those
// quickly. Oldest-out when full, which for a TTL this short is nearly always an
// entry that had expired anyway.
const substrateCacheEntries = 64

// cachedAnswer is a computed result and the instant it was computed at.
type cachedAnswer struct {
	value      any
	computedAt time.Time
}

// substrateCache memoises the substrate reads for substrateCacheTTL.
//
// The singleflight group is the other half of the point: without it, the three
// reads a page load fires concurrently would each start their own walk before any
// of them had a result to cache.
type substrateCache struct {
	mutex   sync.Mutex
	entries map[string]cachedAnswer
	group   singleflight.Group
	// now is injectable so the tests can move time without sleeping.
	now func() time.Time
}

func newSubstrateCache() *substrateCache {
	return &substrateCache{entries: map[string]cachedAnswer{}, now: time.Now}
}

// get returns the answer for key, computing it only when there is no fresh one.
//
// Returns the value and the instant it was computed, which is not necessarily now —
// that difference is the whole reason this returns two things.
func (c *substrateCache) get(ctx context.Context, key string, compute func() (any, error)) (any, time.Time, error) {
	if c == nil {
		value, err := compute()
		return value, time.Now(), err
	}

	if answer, ok := c.fresh(key); ok {
		return answer.value, answer.computedAt, nil
	}

	// Shared: concurrent callers asking the same question wait for one walk rather
	// than starting one each.
	result, err, _ := c.group.Do(key, func() (any, error) {
		// Re-checked inside the flight: a caller that queued behind another one may
		// find the answer already stored by the time it runs.
		if answer, ok := c.fresh(key); ok {
			return answer, nil
		}
		value, err := compute()
		if err != nil {
			return cachedAnswer{}, err
		}
		answer := cachedAnswer{value: value, computedAt: c.now()}
		c.store(key, answer)
		return answer, nil
	})
	if err != nil {
		return nil, time.Time{}, err
	}
	if ctx.Err() != nil {
		return nil, time.Time{}, ctx.Err()
	}
	answer := result.(cachedAnswer)
	return answer.value, answer.computedAt, nil
}

func (c *substrateCache) fresh(key string) (cachedAnswer, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	answer, ok := c.entries[key]
	if !ok || c.now().Sub(answer.computedAt) > substrateCacheTTL {
		return cachedAnswer{}, false
	}
	return answer, true
}

func (c *substrateCache) store(key string, answer cachedAnswer) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if len(c.entries) >= substrateCacheEntries {
		// Drop the oldest rather than clearing everything: clearing would throw away
		// the entry the current burst of requests is about to ask for again.
		var oldestKey string
		var oldest time.Time
		for candidate, entry := range c.entries {
			if oldestKey == "" || entry.computedAt.Before(oldest) {
				oldestKey, oldest = candidate, entry.computedAt
			}
		}
		delete(c.entries, oldestKey)
	}
	c.entries[key] = answer
}

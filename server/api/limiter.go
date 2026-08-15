// server/api/limiter.go
package api

import (
	"context"
	"sync"
	"time"
)

// SearchLimiter enforces "at most one search per interval" across every
// path that can talk to the IRC search bot (browser websocket client and
// the API worker share one instance), so combined traffic never exceeds
// what --rate-limit promised the IRC admins.
type SearchLimiter struct {
	mu       sync.Mutex
	last     time.Time
	interval time.Duration
	now      func() time.Time
}

func NewSearchLimiter(interval time.Duration) *SearchLimiter {
	return &SearchLimiter{interval: interval, now: time.Now}
}

// TryAcquire reports whether a search may be sent now. On success it
// records the send time and returns (0, true). Otherwise it returns the
// remaining wait and false without recording anything.
func (l *SearchLimiter) TryAcquire() (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	next := l.last.Add(l.interval)
	if now.Before(next) {
		return next.Sub(now), false
	}
	l.last = now
	return 0, true
}

// Wait blocks until a search slot is acquired or ctx is done.
func (l *SearchLimiter) Wait(ctx context.Context) error {
	for {
		wait, ok := l.TryAcquire()
		if ok {
			return nil
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

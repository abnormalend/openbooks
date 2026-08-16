// server/api/limiter_test.go
package api

import (
	"context"
	"testing"
	"time"
)

func TestSearchLimiterFirstAcquireIsFree(t *testing.T) {
	l := NewSearchLimiter(10 * time.Second)
	if wait, ok := l.TryAcquire(); !ok || wait != 0 {
		t.Fatalf("first TryAcquire = (%v,%v), want (0,true)", wait, ok)
	}
}

func TestSearchLimiterSecondAcquireWaits(t *testing.T) {
	l := NewSearchLimiter(10 * time.Second)
	l.TryAcquire()
	wait, ok := l.TryAcquire()
	if ok {
		t.Fatal("second TryAcquire should be rate limited")
	}
	if wait <= 9*time.Second || wait > 10*time.Second {
		t.Errorf("wait = %v, want ~10s", wait)
	}
}

func TestSearchLimiterWaitBlocksThenAcquires(t *testing.T) {
	l := NewSearchLimiter(50 * time.Millisecond)
	l.TryAcquire()
	start := time.Now()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("Wait returned after %v, expected to block ~50ms", elapsed)
	}
}

func TestSearchLimiterWaitHonoursContext(t *testing.T) {
	l := NewSearchLimiter(time.Hour)
	l.TryAcquire()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := l.Wait(ctx); err == nil {
		t.Fatal("Wait should return ctx error")
	}
}

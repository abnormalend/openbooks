package api

import (
	"sync"
	"testing"
	"time"
)

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestServerStatsHealthTable(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	cases := []struct {
		name   string
		setup  func(s *ServerStats)
		online map[string]bool
		want   string
	}{
		{"no attempts", func(s *ServerStats) {}, map[string]bool{"A": true}, "unknown"},
		{"healthy recent success", func(s *ServerStats) {
			s.RecordAttempt("A")
			s.RecordComplete("A", 5*time.Second)
		}, map[string]bool{"A": true}, "healthy"},
		{"down offline", func(s *ServerStats) {
			s.RecordAttempt("A")
			s.RecordComplete("A", time.Second)
		}, map[string]bool{"A": false}, "down"},
		{"down fail streak no recent success", func(s *ServerStats) {
			for i := 0; i < 3; i++ {
				s.RecordAttempt("A")
				s.RecordFailure("A", "timeout")
			}
		}, map[string]bool{"A": true}, "down"},
		{"degraded low success rate", func(s *ServerStats) {
			s.RecordAttempt("A")
			s.RecordComplete("A", time.Second) // 1 ok
			s.RecordAttempt("A")
			s.RecordFailure("A", "dcc_failed") // then a fail => streak 1, rate 1/2 < .6
			s.RecordAttempt("A")
			s.RecordFailure("A", "dcc_failed") // rate 1/3
		}, map[string]bool{"A": true}, "degraded"},
		{"cancelled excluded from rate", func(s *ServerStats) {
			s.RecordAttempt("A")
			s.RecordComplete("A", time.Second)
			s.RecordAttempt("A")
			s.RecordCancelled("A") // cancel shouldn't drop health
		}, map[string]bool{"A": true}, "healthy"},
	}
	for _, c := range cases {
		s := NewServerStats()
		s.now = fixedNow(base)
		c.setup(s)
		got := s.Snapshot(c.online)
		var h string
		for _, r := range got {
			if r.Server == "A" {
				h = r.Health
			}
		}
		if h != c.want {
			t.Errorf("%s: health = %q, want %q", c.name, h, c.want)
		}
	}
}

func TestServerStatsRecordsQueuePositionAndCounts(t *testing.T) {
	s := NewServerStats()
	s.RecordAttempt("DV8")
	s.RecordQueuePosition("DV8", 5)
	s.RecordComplete("DV8", 10*time.Second)
	snap := s.Snapshot(map[string]bool{"DV8": true})
	r := snap[0]
	if r.Attempts != 1 || r.Completed != 1 || r.LastQueuePosition != 5 || r.AvgCompleteSeconds != 10 {
		t.Errorf("record = %+v", r)
	}
	if r.LastSuccessAt == nil || r.LastQueuePositionAt == nil {
		t.Errorf("timestamps not set: %+v", r)
	}
}

func TestServerStatsConcurrent(t *testing.T) {
	s := NewServerStats()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.RecordAttempt("X")
			s.RecordQueuePosition("X", 1)
			s.RecordComplete("X", time.Second)
			_ = s.Snapshot(map[string]bool{"X": true})
		}()
	}
	wg.Wait()
	if s.Snapshot(map[string]bool{"X": true})[0].Attempts != 20 {
		t.Errorf("lost updates")
	}
}

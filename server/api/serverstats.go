package api

import (
	"sort"
	"sync"
	"time"
)

const (
	healthWindow        = time.Hour
	downFailStreak      = 3
	degradedSuccessRate = 0.60
)

// ServerStat is a snapshot of one book server's observed behavior.
type ServerStat struct {
	Server              string         `json:"server"`
	Online              bool           `json:"online"`
	Attempts            int            `json:"attempts"`
	Completed           int            `json:"completed"`
	Failed              int            `json:"failed"`
	FailuresByCode      map[string]int `json:"failuresByCode"`
	LastQueuePosition   int            `json:"lastQueuePosition"`
	LastQueuePositionAt *time.Time     `json:"lastQueuePositionAt"`
	RecentFailStreak    int            `json:"recentFailStreak"`
	LastSuccessAt       *time.Time     `json:"lastSuccessAt"`
	LastFailureAt       *time.Time     `json:"lastFailureAt"`
	AvgCompleteSeconds  float64        `json:"avgCompleteSeconds"`
	Health              string         `json:"health"`
}

// ServerStats accumulates per-server outcomes in memory (v1: no persistence).
type ServerStats struct {
	mu  sync.Mutex
	m   map[string]*ServerStat
	now func() time.Time
}

func NewServerStats() *ServerStats {
	return &ServerStats{m: make(map[string]*ServerStat), now: time.Now}
}

func (s *ServerStats) get(server string) *ServerStat {
	st, ok := s.m[server]
	if !ok {
		st = &ServerStat{Server: server, FailuresByCode: make(map[string]int)}
		s.m[server] = st
	}
	return st
}

func (s *ServerStats) RecordAttempt(server string) {
	if server == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.get(server).Attempts++
}

func (s *ServerStats) RecordQueuePosition(server string, pos int) {
	if server == "" || pos <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.get(server)
	now := s.now()
	st.LastQueuePosition = pos
	st.LastQueuePositionAt = &now
}

func (s *ServerStats) RecordComplete(server string, dur time.Duration) {
	if server == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.get(server)
	now := s.now()
	st.Completed++
	st.RecentFailStreak = 0
	st.LastSuccessAt = &now
	// rolling average
	secs := dur.Seconds()
	if st.Completed == 1 {
		st.AvgCompleteSeconds = secs
	} else {
		st.AvgCompleteSeconds += (secs - st.AvgCompleteSeconds) / float64(st.Completed)
	}
}

func (s *ServerStats) RecordFailure(server, code string) {
	if server == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.get(server)
	now := s.now()
	st.Failed++
	st.FailuresByCode[code]++
	st.RecentFailStreak++
	st.LastFailureAt = &now
}

// RecordCancelled counts a user-initiated cancel; weighted lightly (excluded
// from the success-rate denominator and does not bump the fail streak).
func (s *ServerStats) RecordCancelled(server string) {
	if server == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.get(server)
	st.Failed++
	st.FailuresByCode["cancelled"]++
}

// Snapshot returns per-server copies with Online merged from the presence map
// and Health computed, sorted by health rank then name. Servers present in
// the online map but never yet attempted (cold start, no history) are still
// included with zero counts so they surface as neutral "unknown" rather than
// being silently omitted from the ranked list.
func (s *ServerStats) Snapshot(online map[string]bool) []ServerStat {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ServerStat, 0, len(s.m)+len(online))
	seen := make(map[string]bool, len(s.m))
	for name, st := range s.m {
		c := *st
		c.FailuresByCode = make(map[string]int, len(st.FailuresByCode))
		for k, v := range st.FailuresByCode {
			c.FailuresByCode[k] = v
		}
		// Copy the *time.Time fields to fresh pointers so the snapshot
		// never aliases the live ServerStat's pointers (mirrors the
		// FailuresByCode deep copy above).
		if st.LastQueuePositionAt != nil {
			t := *st.LastQueuePositionAt
			c.LastQueuePositionAt = &t
		}
		if st.LastSuccessAt != nil {
			t := *st.LastSuccessAt
			c.LastSuccessAt = &t
		}
		if st.LastFailureAt != nil {
			t := *st.LastFailureAt
			c.LastFailureAt = &t
		}
		c.Online = online[name]
		c.Health = s.health(&c)
		out = append(out, c)
		seen[name] = true
	}
	for name, isOnline := range online {
		if seen[name] || !isOnline {
			continue
		}
		c := ServerStat{Server: name, Online: true, FailuresByCode: map[string]int{}}
		c.Health = s.health(&c)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := healthRank(out[i].Health), healthRank(out[j].Health)
		if ri != rj {
			return ri < rj
		}
		return out[i].Server < out[j].Server
	})
	return out
}

func healthRank(h string) int {
	switch h {
	case "healthy":
		return 0
	case "unknown":
		return 1
	case "degraded":
		return 2
	default: // down
		return 3
	}
}

func (s *ServerStats) health(c *ServerStat) string {
	// unknown means "no terminal outcome yet" — this also covers a
	// cold-start server whose first request is still in flight
	// (Attempts=1, Completed=0, Failed=0), not just Attempts==0. A
	// server with a request in flight and no history must not fall
	// through to "degraded", which the MCP ranks below "unknown" and
	// would penalize the cold-start case the neutral-unknown ranking
	// exists to protect.
	if c.Completed == 0 && c.Failed == 0 {
		return "unknown"
	}
	now := s.now()
	recentSuccess := c.LastSuccessAt != nil && now.Sub(*c.LastSuccessAt) <= healthWindow
	if !c.Online || (c.RecentFailStreak >= downFailStreak && !recentSuccess) {
		return "down"
	}
	nonCancelledFail := c.Failed - c.FailuresByCode["cancelled"]
	denom := c.Completed + nonCancelledFail
	// A server with no completions and no failures never reaches this
	// line (caught by the unknown check above), so 0.0 here is a safe,
	// spec-aligned default that can't accidentally read as "healthy" —
	// the healthy branch also requires recentSuccess, i.e. Completed>=1.
	rate := 0.0
	if denom > 0 {
		rate = float64(c.Completed) / float64(denom)
	}
	if recentSuccess && rate >= degradedSuccessRate {
		return "healthy"
	}
	return "degraded"
}

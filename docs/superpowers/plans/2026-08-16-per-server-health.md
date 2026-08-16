# Per-Server Health / Queue Signal — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: subagent-driven-development or executing-plans. Checkbox steps.

**Goal:** Passive per-server reliability stats derived from the API's own download outcomes + queue notices, exposed at `GET /api/server-stats` and via an MCP `server_stats()` tool, so clients rank servers by observed health instead of folklore. In-memory v1.

**Spec:** `docs/superpowers/specs/2026-08-16-per-server-health.md` (read it — the pinned health constants and the neutral-`unknown` ranking are authoritative).

**Conventions:** repo root `/home/brent/openbooks`; `gofmt -l ./...` empty + `go vet ./...` clean before each commit; commit trailers:
```
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01PHyjTkfDAfEjj8Jaoeqdd4
```
Branch `feature/per-server-health` (checked out). Do not push. `/api/servers` MUST stay unchanged.

---

### Task 1: `serverstats` store + health heuristic

**Files:** `server/api/serverstats.go`, `server/api/serverstats_test.go`

- [ ] **Step 1 — failing tests** (`server/api/serverstats_test.go`):

```go
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
			s.RecordAttempt("A"); s.RecordComplete("A", 5*time.Second)
		}, map[string]bool{"A": true}, "healthy"},
		{"down offline", func(s *ServerStats) {
			s.RecordAttempt("A"); s.RecordComplete("A", time.Second)
		}, map[string]bool{"A": false}, "down"},
		{"down fail streak no recent success", func(s *ServerStats) {
			for i := 0; i < 3; i++ { s.RecordAttempt("A"); s.RecordFailure("A", "timeout") }
		}, map[string]bool{"A": true}, "down"},
		{"degraded low success rate", func(s *ServerStats) {
			s.RecordAttempt("A"); s.RecordComplete("A", time.Second)     // 1 ok
			s.RecordAttempt("A"); s.RecordFailure("A", "dcc_failed")     // then a fail => streak 1, rate 1/2 < .6
			s.RecordAttempt("A"); s.RecordFailure("A", "dcc_failed")     // rate 1/3
		}, map[string]bool{"A": true}, "degraded"},
		{"cancelled excluded from rate", func(s *ServerStats) {
			s.RecordAttempt("A"); s.RecordComplete("A", time.Second)
			s.RecordAttempt("A"); s.RecordCancelled("A")                 // cancel shouldn't drop health
		}, map[string]bool{"A": true}, "healthy"},
	}
	for _, c := range cases {
		s := NewServerStats()
		s.now = fixedNow(base)
		c.setup(s)
		got := s.Snapshot(c.online)
		var h string
		for _, r := range got {
			if r.Server == "A" { h = r.Health }
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
			s.RecordAttempt("X"); s.RecordQueuePosition("X", 1); s.RecordComplete("X", time.Second)
			_ = s.Snapshot(map[string]bool{"X": true})
		}()
	}
	wg.Wait()
	if s.Snapshot(map[string]bool{"X": true})[0].Attempts != 20 {
		t.Errorf("lost updates")
	}
}
```

- [ ] **Step 2 — run:** `go test -race -run TestServerStats ./server/api/ 2>&1 | head -3` → undefined.

- [ ] **Step 3 — implement** (`server/api/serverstats.go`):

```go
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
// and Health computed, sorted by health rank then name.
func (s *ServerStats) Snapshot(online map[string]bool) []ServerStat {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ServerStat, 0, len(s.m))
	for name, st := range s.m {
		c := *st
		c.FailuresByCode = make(map[string]int, len(st.FailuresByCode))
		for k, v := range st.FailuresByCode {
			c.FailuresByCode[k] = v
		}
		c.Online = online[name]
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
	if c.Attempts == 0 {
		return "unknown"
	}
	now := s.now()
	recentSuccess := c.LastSuccessAt != nil && now.Sub(*c.LastSuccessAt) <= healthWindow
	if !c.Online || (c.RecentFailStreak >= downFailStreak && !recentSuccess) {
		return "down"
	}
	nonCancelledFail := c.Failed - c.FailuresByCode["cancelled"]
	denom := c.Completed + nonCancelledFail
	rate := 1.0
	if denom > 0 {
		rate = float64(c.Completed) / float64(denom)
	}
	if recentSuccess && rate >= degradedSuccessRate {
		return "healthy"
	}
	return "degraded"
}
```

- [ ] **Step 4 — run:** `go test -race -count=3 ./server/api/` → ok. Adjust the degraded/healthy test expectations only if a threshold boundary is off — the classification code is authoritative, keep it matching the spec.
- [ ] **Step 5 — commit:** `git add server/api/serverstats.go server/api/serverstats_test.go && git commit -m "feat(api): per-server stats store with pinned health heuristic"`

---

### Task 2: Record outcomes from the download worker

**Files:** `server/api/worker.go`, `server/api/api.go`, `server/api/worker_test.go`

- [ ] **Step 1 — failing test** append to `server/api/worker_test.go`:

```go
func TestDownloadRecordsServerStats(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	stats := NewServerStats()
	w.stats = stats
	w.fetchCtx = func(ctx context.Context, baseDir, dccStr string, progress io.Writer) (string, error) {
		p := filepath.Join(baseDir, "b.epub")
		os.MkdirAll(baseDir, 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
		return p, nil
	}
	sess.downloadReply = []Event{
		{Kind: EvQueuePosition, Text: "queueposition 4"},
		{Kind: EvBookResult, Text: "DCC SEND b.epub 2130706433 6669 1"},
	}
	job := NewDownloadJob("!DV8 Author - Title.epub")
	reg.Enqueue(job)
	ctx, cancel := context.WithCancel(context.Background())
	go w.RunDownload(ctx)
	waitStatus(t, reg, job, StatusComplete)
	cancel()

	snap := stats.Snapshot(map[string]bool{"DV8": true})
	if len(snap) != 1 || snap[0].Server != "DV8" {
		t.Fatalf("snap = %+v", snap)
	}
	r := snap[0]
	if r.Attempts != 1 || r.Completed != 1 || r.LastQueuePosition != 4 {
		t.Errorf("stat = %+v", r)
	}
}

func TestDownloadServerUnavailableRecordsFailure(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	stats := NewServerStats()
	w.stats = stats
	sess.downloadReply = []Event{{Kind: EvBadServer}}
	job := NewDownloadJob("!Bsk Author - Title.epub")
	reg.Enqueue(job)
	ctx, cancel := context.WithCancel(context.Background())
	go w.RunDownload(ctx)
	waitStatus(t, reg, job, StatusError)
	cancel()
	r := stats.Snapshot(map[string]bool{})[0]
	if r.FailuresByCode["server_unavailable"] != 1 || r.Failed != 1 {
		t.Errorf("stat = %+v", r)
	}
}

func TestBrowserBlockDoesNotBlameServer(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	stats := NewServerStats()
	w.stats = stats
	w.cfg.BrowserConnected = func() bool { return true }
	job := NewDownloadJob("!DV8 Author - Title.epub")
	reg.Enqueue(job)
	ctx, cancel := context.WithCancel(context.Background())
	go w.RunDownload(ctx)
	waitStatus(t, reg, job, StatusError)
	cancel()
	// browser_session_active is our-side; the server must not be recorded.
	if len(stats.Snapshot(map[string]bool{})) != 0 {
		t.Errorf("server wrongly recorded: %+v", stats.Snapshot(map[string]bool{}))
	}
}
```

- [ ] **Step 2 — run:** fails (`w.stats` undefined).

- [ ] **Step 3 — implement** in `server/api/worker.go`:
  - Add `stats *ServerStats` to `Worker` and `WorkerConfig`? Put the pointer on `Worker` directly, set from `WorkerConfig.Stats` in `NewWorker` (add `Stats *ServerStats` to `WorkerConfig`). In `newTestWorker`, leave it nil-safe: guard all `w.stats` calls with `if w.stats != nil`.
  - Add a helper `serverOf(book string) string` = `book[1:idx]` where `idx = strings.Index(book, " ")` (return "" if malformed).
  - In `doDownload`: after the successful `w.sess.DownloadBook(job.Book)` send (only then have we actually asked the server), compute `server := serverOf(job.Book)`, `start := time.Now()`, and `if w.stats != nil { w.stats.RecordAttempt(server) }`. Set a bool `attempted = true`.
  - Add a deferred outcome recorder BEFORE the send is fine but it must only fire when `attempted`:
    ```go
    attempted := false
    var server string
    var start time.Time
    defer func() {
        if !attempted || w.stats == nil {
            return
        }
        snap, _ := w.reg.Get(job.ID)
        switch snap.Status {
        case StatusComplete:
            w.stats.RecordComplete(server, time.Since(start))
        case StatusCancelled:
            w.stats.RecordCancelled(server)
        case StatusError:
            code := "error"
            if snap.Error != nil {
                code = snap.Error.Code
            }
            // our-side pre-server failures are excluded by only recording when attempted
            w.stats.RecordFailure(server, code)
        }
    }()
    ```
    Set `server`/`start`/`attempted` right after the send succeeds. (browser_session_active and irc_connect_failed both `return` before the send, so `attempted` stays false → not recorded — satisfies TestBrowserBlockDoesNotBlameServer.)
  - In the `EvQueuePosition` case, after recording on the job, also `if w.stats != nil { w.stats.RecordQueuePosition(server, pos) }`.
- [ ] **Step 4 — wire in `server/api/api.go`:** add `stats *ServerStats` field to `API`, create it in `New` (`stats: NewServerStats()`), pass into `WorkerConfig{Stats: a.stats, ...}` (add the field). (Endpoint added in Task 3.)
- [ ] **Step 5 — run:** `gofmt -l ./server && go vet ./server/api/ && go test -race -count=3 ./server/api/` → clean/ok. Full `go test -race ./...` green.
- [ ] **Step 6 — commit:** `git add server/api/worker.go server/api/api.go server/api/worker_test.go && git commit -m "feat(api): record per-server download outcomes"`

---

### Task 3: `GET /api/server-stats` endpoint + OpenAPI

**Files:** `server/api/api.go`, `server/api/api_test.go`, `server/api/openapi.yaml`

- [ ] **Step 1 — failing test** append to `server/api/api_test.go`:

```go
func TestServerStatsEndpoint(t *testing.T) {
	a := newTestAPI(t, false)
	// seed some stats directly
	a.stats.RecordAttempt("DV8")
	a.stats.RecordComplete("DV8", 3*time.Second)
	a.stats.RecordAttempt("Bsk")
	a.stats.RecordFailure("Bsk", "server_unavailable")
	rec, m := call(t, a, "GET", "/server-stats", "", "tok")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	servers := m["servers"].([]interface{})
	if len(servers) != 2 {
		t.Fatalf("servers = %v", servers)
	}
	// unauth → 401
	rec, _ = call(t, a, "GET", "/server-stats", "", "")
	if rec.Code != 401 {
		t.Errorf("unauth = %d", rec.Code)
	}
}
```

(`newTestAPI` must expose `a.stats` — it already holds the `*API`; ensure the field is reachable from the test, same package.)

- [ ] **Step 2 — run:** fails (404/no field).
- [ ] **Step 3 — implement:** in `Router()` protected group add `p.Get("/server-stats", a.serverStats)`. Handler:
```go
func (a *API) serverStats(w http.ResponseWriter, _ *http.Request) {
	online := map[string]bool{}
	for _, name := range a.deps.Servers().ElevatedUsers {
		online[name] = true
	}
	writeJSON(w, 200, map[string]interface{}{"servers": a.stats.Snapshot(online)})
}
```
- [ ] **Step 4 — openapi.yaml:** add `GET /server-stats` (auth; 200 `{servers:[ServerStat]}`, 401, 503) and a `ServerStat` schema with the fields + a note that `lastQueuePosition` is our last observed position, not live depth.
- [ ] **Step 5 — run:** `gofmt -l ./server && go vet ./server/api/ && go test -race ./server/api/` → ok; `python3 -c "import yaml; yaml.safe_load(open('server/api/openapi.yaml'))"` (skip if no PyYAML).
- [ ] **Step 6 — commit:** `git add server/api/api.go server/api/api_test.go server/api/openapi.yaml && git commit -m "feat(api): GET /api/server-stats endpoint"`

---

### Task 4: MCP `server_stats()` tool + ranking guidance

**Files:** `~/openbooks-mcp/openbooks_mcp_server/server.py`, `tests/test_server.py`, `README.md`

- [ ] **Step 1 — failing test** in `~/openbooks-mcp/tests/test_server.py`: extend `FakeAPI.handler` to answer `GET /server-stats` → 200 `{"servers":[{"server":"DV8","health":"healthy",...},{"server":"Bsk","health":"down",...}]}`. Add `test_server_stats` asserting the tool returns the list.
- [ ] **Step 2 — implement:** client `server_stats()` → `GET /server-stats`; `@mcp.tool() def server_stats()` returning it. Update the `search_books` docstring / README with the ranking rule: prefer `healthy`, then `unknown` (neutral — do NOT skip cold-start servers), then `degraded`, avoid `down`; tie-break lower `lastQueuePosition` then higher `successRate`.
- [ ] **Step 3 — run:** `/home/brent/.local/share/pipx/venvs/stash-mcp-server/bin/python -m pytest -q tests/` → all pass. Commit in `~/openbooks-mcp`: `mcp: server_stats tool + health-ranked search guidance`.

---

### Task 5: Docs + integration test + PR

**Files:** `docs/docs/api.md`, `tests/api_integration_test.go`

- [ ] **Step 1 — docs:** `docs/docs/api.md` gains a "Server health" section: the `GET /api/server-stats` shape, the health labels + what drives them, and the explicit caveat that `lastQueuePosition` is our last observed position (not live depth) and that `/api/servers` is unchanged.
- [ ] **Step 2 — integration** (`tests/api_integration_test.go`): after the existing end-to-end download completes, `GET /api/server-stats` and assert the server that served the download shows `completed >= 1` and a sane `health`. (Reuse the running server + token; no new fakes needed.)
- [ ] **Step 3 — full suite:** `gofmt -l .; go vet ./...; go test -race ./...; go test -race -tags=integration ./...` all green.
- [ ] **Step 4 — commit + PR** against `master`, title `feat(api): per-server health/queue signal (GET /api/server-stats)`, body summarizing the approach (passive stats, honest "not live depth" caveat, neutral unknown), spec+plan links, and a test-plan note (in-memory v1; MCP ranking). Post a `DONE:` line to `/home/brent/hermes-handoffs/openbooks-COMMS.md` with the PR URL.

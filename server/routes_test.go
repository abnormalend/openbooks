// server/routes_test.go
package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// silentIRC is a TCP listener that accepts connections and never speaks.
// core.Join succeeds against it (dial + 2s sleep + JOIN), so an API search
// job stays "running" until its timeout — which makes "API busy" states
// deterministic in tests.
func silentIRC(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) { io.Copy(io.Discard, c) }(c)
		}
	}()
	return l.Addr().String()
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := NewHandler(ctx, Config{
		Basepath:         "/openbooks/",
		DownloadDir:      t.TempDir(),
		SearchTimeout:    10 * time.Second,
		UserName:         "tester",
		Server:           silentIRC(t),
		APIToken:         "tok",
		Version:          "test",
		SearchJobTimeout: 30 * time.Second,
	})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

func TestAPIMountedUnderBasepath(t *testing.T) {
	ts := newTestServer(t)
	res, err := http.Get(ts.URL + "/openbooks/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("health = %d", res.StatusCode)
	}
	var m map[string]interface{}
	json.NewDecoder(res.Body).Decode(&m)
	if m["basePath"] != "/openbooks/" || m["version"] != "test" {
		t.Errorf("health = %v", m)
	}
}

func TestAPIRejectsUnauthenticated(t *testing.T) {
	ts := newTestServer(t)
	res, _ := http.Post(ts.URL+"/openbooks/api/search", "application/json", strings.NewReader(`{"query":"x"}`))
	if res.StatusCode != 401 {
		t.Errorf("status = %d, want 401", res.StatusCode)
	}
}

func TestWebsocketRefusedWhileAPIJobQueued(t *testing.T) {
	ts := newTestServer(t)
	req, _ := http.NewRequest("POST", ts.URL+"/openbooks/api/search", strings.NewReader(`{"query":"x"}`))
	req.Header.Set("Authorization", "Bearer tok")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 202 {
		t.Fatalf("enqueue = %d", res.StatusCode)
	}
	// Plain GET on /ws (no upgrade headers) is enough to hit the guard,
	// which runs before the websocket upgrade.
	res, _ = http.Get(ts.URL + "/openbooks/ws")
	if res.StatusCode != 409 {
		t.Errorf("/ws while API busy = %d, want 409", res.StatusCode)
	}
}

func TestLegacyRoutesStillWork(t *testing.T) {
	ts := newTestServer(t)
	res, _ := http.Get(ts.URL + "/openbooks/servers")
	if res.StatusCode != 200 {
		t.Errorf("/servers = %d", res.StatusCode)
	}
	res, _ = http.Get(ts.URL + "/openbooks/stats")
	if res.StatusCode != 200 {
		t.Errorf("/stats = %d", res.StatusCode)
	}
}

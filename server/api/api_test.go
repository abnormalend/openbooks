// server/api/api_test.go
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evan-buss/openbooks/core"
)

func newTestAPI(t *testing.T, browser bool) *API {
	t.Helper()
	return New(Config{
		Token:       "tok",
		Version:     "9.9.9",
		BasePath:    "/openbooks/",
		DownloadDir: "/books",
		QueueDepth:  2,
		JobTTL:      time.Hour,
		Session:     SessionConfig{Nick: "n", Server: "127.0.0.1:1"},
	}, Deps{
		Limiter:          NewSearchLimiter(10 * time.Second),
		BrowserConnected: func() bool { return browser },
		Servers:          func() core.IrcServers { return core.IrcServers{ElevatedUsers: []string{"DV8"}} },
		Log:              log.New(io.Discard, "", 0),
	})
}

func call(t *testing.T, a *API, method, path, body, token string) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, req)
	var m map[string]interface{}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("non-JSON body %q: %v", rec.Body.String(), err)
		}
	}
	return rec, m
}

func TestHealthNoAuth(t *testing.T) {
	a := newTestAPI(t, false)
	rec, m := call(t, a, "GET", "/health", "", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body)
	}
	if m["version"] != "9.9.9" || m["basePath"] != "/openbooks/" || m["downloadDir"] != "/books" {
		t.Errorf("health = %v", m)
	}
	if m["apiEnabled"] != true || m["ircConnected"] != false || m["browserConnected"] != false {
		t.Errorf("health flags = %v", m)
	}
	s := m["search"].(map[string]interface{})
	if s["running"] != false || s["queued"] != float64(0) {
		t.Errorf("health.search = %v", s)
	}
	if _, ok := m["uptimeSeconds"]; !ok {
		t.Error("missing uptimeSeconds")
	}
}

func TestProtectedRoutesRequireToken(t *testing.T) {
	a := newTestAPI(t, false)
	for _, p := range []string{"/search", "/download", "/jobs", "/servers", "/search/x", "/download/x"} {
		rec, _ := call(t, a, "GET", p, "", "")
		if rec.Code != 401 {
			t.Errorf("GET %s without token = %d, want 401", p, rec.Code)
		}
	}
}

func TestPostSearchQueuesAndPolls(t *testing.T) {
	a := newTestAPI(t, false)
	rec, m := call(t, a, "POST", "/search", `{"query":"fourth wing","limit":5}`, "tok")
	if rec.Code != 202 {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body)
	}
	if m["status"] != "queued" || m["position"] != float64(1) || m["jobId"] == "" {
		t.Errorf("accepted body = %v", m)
	}
	id := m["jobId"].(string)

	rec, m = call(t, a, "GET", "/search/"+id, "", "tok")
	if rec.Code != 200 || m["query"] != "fourth wing" || m["limit"] != float64(5) || m["status"] != "queued" {
		t.Errorf("poll = %d %v", rec.Code, m)
	}
	// Type mismatch → 404
	rec, _ = call(t, a, "GET", "/download/"+id, "", "tok")
	if rec.Code != 404 {
		t.Errorf("download/{searchId} = %d, want 404", rec.Code)
	}
	// Bad / unknown ids → 404
	for _, bad := range []string{"/search/not-a-uuid", "/search/00000000-0000-0000-0000-000000000000"} {
		if rec, _ := call(t, a, "GET", bad, "", "tok"); rec.Code != 404 {
			t.Errorf("GET %s = %d, want 404", bad, rec.Code)
		}
	}
	// Health reflects the queue.
	_, h := call(t, a, "GET", "/health", "", "")
	if h["search"].(map[string]interface{})["queued"] != float64(1) {
		t.Errorf("health.search.queued = %v", h["search"])
	}
}

func TestPostSearchValidation(t *testing.T) {
	a := newTestAPI(t, false)
	for _, body := range []string{``, `{}`, `{"query":""}`, `{"query":"  "}`, `not json`, `{"query":"x","limit":-1}`} {
		rec, m := call(t, a, "POST", "/search", body, "tok")
		if rec.Code != 400 || m["code"] != "bad_request" {
			t.Errorf("body %q → %d %v, want 400 bad_request", body, rec.Code, m)
		}
	}
}

func TestPostSearchQueueFull(t *testing.T) {
	a := newTestAPI(t, false) // depth 2
	call(t, a, "POST", "/search", `{"query":"a"}`, "tok")
	call(t, a, "POST", "/search", `{"query":"b"}`, "tok")
	rec, m := call(t, a, "POST", "/search", `{"query":"c"}`, "tok")
	if rec.Code != 409 || m["code"] != "queue_full" {
		t.Errorf("third = %d %v", rec.Code, m)
	}
}

func TestPostSearchBrowserActive(t *testing.T) {
	a := newTestAPI(t, true)
	rec, m := call(t, a, "POST", "/search", `{"query":"a"}`, "tok")
	if rec.Code != 409 || m["code"] != "browser_session_active" {
		t.Errorf("= %d %v", rec.Code, m)
	}
	rec, m = call(t, a, "POST", "/download", `{"book":"!x y.epub"}`, "tok")
	if rec.Code != 409 || m["code"] != "browser_session_active" {
		t.Errorf("= %d %v", rec.Code, m)
	}
}

func TestPostDownloadValidationAndPoll(t *testing.T) {
	a := newTestAPI(t, false)
	for _, body := range []string{``, `{}`, `{"book":""}`, `{"book":"no bang"}`} {
		rec, _ := call(t, a, "POST", "/download", body, "tok")
		if rec.Code != 400 {
			t.Errorf("body %q → %d, want 400", body, rec.Code)
		}
	}
	rec, m := call(t, a, "POST", "/download", `{"book":"!DV8 A - B.epub"}`, "tok")
	if rec.Code != 202 {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body)
	}
	id := m["jobId"].(string)
	rec, m = call(t, a, "GET", "/download/"+id, "", "tok")
	if rec.Code != 200 || m["book"] != "!DV8 A - B.epub" || m["bytes"] != float64(0) {
		t.Errorf("poll = %d %v", rec.Code, m)
	}
	if _, ok := m["path"]; !ok {
		t.Errorf("path must be present (null) while queued: %v", m)
	}
}

func TestListJobsAndFilter(t *testing.T) {
	a := newTestAPI(t, false)
	call(t, a, "POST", "/search", `{"query":"a"}`, "tok")
	call(t, a, "POST", "/download", `{"book":"!b c.epub"}`, "tok")
	_, m := call(t, a, "GET", "/jobs", "", "tok")
	if len(m["jobs"].([]interface{})) != 2 {
		t.Errorf("jobs = %v", m)
	}
	_, m = call(t, a, "GET", "/jobs?type=download", "", "tok")
	if len(m["jobs"].([]interface{})) != 1 {
		t.Errorf("filtered jobs = %v", m)
	}
	rec, _ := call(t, a, "GET", "/jobs?type=bogus", "", "tok")
	if rec.Code != 400 {
		t.Errorf("bad type filter = %d, want 400", rec.Code)
	}
}

func TestServersEndpoint(t *testing.T) {
	a := newTestAPI(t, false)
	_, m := call(t, a, "GET", "/servers", "", "tok")
	sv := m["servers"].(map[string]interface{})
	if sv["elevatedUsers"].([]interface{})[0] != "DV8" {
		t.Errorf("servers = %v", m)
	}
}

func TestOpenAPIServedWithoutAuth(t *testing.T) {
	a := newTestAPI(t, false)
	req := httptest.NewRequest("GET", "/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, req)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("openapi:")) {
		t.Errorf("openapi = %d %q", rec.Code, rec.Body.String())
	}
}

func TestYield(t *testing.T) {
	a := newTestAPI(t, false)
	if !a.Yield() {
		t.Error("Yield with no jobs should succeed")
	}
	call(t, a, "POST", "/search", `{"query":"a"}`, "tok")
	if a.Yield() {
		t.Error("Yield with a queued job should refuse")
	}
}

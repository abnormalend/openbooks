//go:build integration

package tests

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evan-buss/openbooks/server"
)

// startApiIrcServer answers @search with a results DCC SEND and any "!"
// line with a book DCC SEND. Handles a single client connection.
func startApiIrcServer(t *testing.T, resultsPort string, resultsSize int, bookPort string, bookSize int) (addr string, stop func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var accepted net.Conn
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := l.Accept()
		if err != nil {
			return
		}
		mu.Lock()
		accepted = conn
		mu.Unlock()
		defer conn.Close()

		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.Contains(line, "@search"):
				fmt.Fprintf(conn, ":search!u@h NOTICE tester :Your search has been accepted\r\n")
				fmt.Fprintf(conn, ":search!u@h PRIVMSG tester :DCC SEND Search_results_for__gatsby.txt.zip 2130706433 %s %d\r\n", resultsPort, resultsSize)
			case strings.HasPrefix(line, "PRIVMSG #ebooks :!"):
				fmt.Fprintf(conn, ":DV8!u@h NOTICE tester :Added to queueposition 2.\r\n")
				fmt.Fprintf(conn, ":DV8!u@h PRIVMSG tester :DCC SEND great-gatsby.epub 2130706433 %s %d\r\n", bookPort, bookSize)
			}
		}
	}()
	return l.Addr().String(), func() {
		l.Close()
		mu.Lock()
		if accepted != nil {
			accepted.Close()
		}
		mu.Unlock()
		wg.Wait()
	}
}

func apiReq(t *testing.T, ts *httptest.Server, method, path, body string) (int, map[string]interface{}) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+"/api"+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer integration-token")
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	var m map[string]interface{}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &m); err != nil {
			t.Fatalf("%s %s: decode body %q: %v", method, path, bodyBytes, err)
		}
	}
	return res.StatusCode, m
}

func pollJob(t *testing.T, ts *httptest.Server, path string, timeout time.Duration) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		code, m := apiReq(t, ts, "GET", path, "")
		if code != 200 {
			t.Fatalf("GET %s = %d %v", path, code, m)
		}
		if m["status"] == "complete" || m["status"] == "error" {
			return m
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish within %v", path, timeout)
	return nil
}

func TestAPISearchThenDownloadEndToEnd(t *testing.T) {
	results := makeZip(t, "results.txt", sampleSearchResults)
	resultsPort, stopResults := startDccServer(t, results)
	defer stopResults()

	book := bytes.Repeat([]byte("E"), 12345)
	bookPort, stopBook := startDccServer(t, book)
	defer stopBook()

	ircAddr, stopIrc := startApiIrcServer(t, resultsPort, len(results), bookPort, len(book))
	defer stopIrc()

	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler := server.NewHandler(ctx, server.Config{
		Basepath:           "/",
		DownloadDir:        dir,
		LibrarySubdir:      "books",
		Persist:            true,
		SearchTimeout:      10 * time.Second,
		UserName:           "tester",
		UserAgent:          "OpenBooks integration",
		Server:             ircAddr,
		EnableTLS:          false,
		SearchBot:          "search",
		APIToken:           "integration-token",
		SearchJobTimeout:   10 * time.Second,
		DownloadJobTimeout: 10 * time.Second,
		Version:            "it",
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Unauthenticated → 401
	res, err := http.Post(ts.URL+"/api/search", "application/json", strings.NewReader(`{"query":"x"}`))
	if err != nil {
		t.Fatalf("POST /api/search (unauth): %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("unauth = %d", res.StatusCode)
	}

	// Search
	code, m := apiReq(t, ts, "POST", "/search", `{"query":"the great gatsby","limit":2}`)
	if code != 202 {
		t.Fatalf("POST /search = %d %v", code, m)
	}
	searchID := m["jobId"].(string)

	// Browser must be refused while the job is live (core.Join sleeps 2s,
	// so the job is still running here).
	wsRes, err := http.Get(ts.URL + "/ws")
	if err != nil {
		t.Fatalf("GET /ws: %v", err)
	}
	wsRes.Body.Close()
	if wsRes.StatusCode != 409 {
		t.Errorf("/ws during API job = %d, want 409", wsRes.StatusCode)
	}

	sj := pollJob(t, ts, "/search/"+searchID, 15*time.Second)
	if sj["status"] != "complete" {
		t.Fatalf("search job = %v", sj)
	}
	resultsArr := sj["results"].([]interface{})
	if len(resultsArr) != 2 {
		t.Fatalf("results = %d, want 2 (limit): %v", len(resultsArr), sj)
	}
	first := resultsArr[0].(map[string]interface{})
	full := first["full"].(string)
	if !strings.HasPrefix(full, "!") {
		t.Fatalf("full = %q", full)
	}

	// Download the first result
	code, m = apiReq(t, ts, "POST", "/download", fmt.Sprintf(`{"book":%q}`, full))
	if code != 202 {
		t.Fatalf("POST /download = %d %v", code, m)
	}
	dj := pollJob(t, ts, "/download/"+m["jobId"].(string), 15*time.Second)
	if dj["status"] != "complete" {
		t.Fatalf("download job = %v", dj)
	}
	if dj["fileName"] != "great-gatsby.epub" || dj["size"] != float64(len(book)) || dj["bytes"] != float64(len(book)) {
		t.Errorf("download job fields = %v", dj)
	}
	if dj["queuePosition"] != float64(2) {
		t.Errorf("queuePosition = %v, want 2", dj["queuePosition"])
	}
	got, err := os.ReadFile(filepath.Join(dir, "books", "great-gatsby.epub"))
	if err != nil {
		t.Fatalf("book not on disk: %v", err)
	}
	if !bytes.Equal(got, book) {
		t.Errorf("book bytes differ: %d vs %d", len(got), len(book))
	}

	// Health reflects a connected, idle API session
	code, health := apiReq(t, ts, "GET", "/health", "")
	if code != 200 || health["ircConnected"] != true {
		t.Errorf("health = %d %v", code, health)
	}

	// Jobs list has both
	_, jl := apiReq(t, ts, "GET", "/jobs", "")
	if len(jl["jobs"].([]interface{})) != 2 {
		t.Errorf("jobs = %v", jl)
	}
}

// startSlowDccServer accepts one connection and dribbles small chunks
// forever (until the connection is closed by the reader or by stop), used
// to simulate an in-flight DCC transfer that never finishes on its own so
// a test can exercise cancelling it mid-transfer.
func startSlowDccServer(t *testing.T) (port string, stop func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen slow dcc: %v", err)
	}
	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 64)
		for {
			select {
			case <-stopCh:
				return
			default:
			}
			if _, err := conn.Write(buf); err != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	return fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port), func() {
		close(stopCh)
		l.Close()
		wg.Wait()
	}
}

// TestAPICancelDownload starts a download whose DCC transfer announces a
// large size but only ever dribbles bytes in slowly, then cancels it via
// DELETE /api/download/{id} and asserts it finalizes as "cancelled" (not
// "complete"/"error") promptly, and that the download queue frees up.
func TestAPICancelDownload(t *testing.T) {
	bookPort, stopBook := startSlowDccServer(t)
	defer stopBook()

	// A large announced size the slow server could never deliver within
	// the test's bound, so the only way this job finishes is cancellation.
	const announcedSize = 50 * 1024 * 1024
	ircAddr, stopIrc := startApiIrcServer(t, "0", 0, bookPort, announcedSize)
	defer stopIrc()

	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler := server.NewHandler(ctx, server.Config{
		Basepath:           "/",
		DownloadDir:        dir,
		LibrarySubdir:      "books",
		Persist:            true,
		SearchTimeout:      10 * time.Second,
		UserName:           "tester",
		UserAgent:          "OpenBooks integration",
		Server:             ircAddr,
		EnableTLS:          false,
		SearchBot:          "search",
		APIToken:           "integration-token",
		SearchJobTimeout:   10 * time.Second,
		DownloadJobTimeout: 10 * time.Second,
		Version:            "it",
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	code, m := apiReq(t, ts, "POST", "/download", `{"book":"!DV8 slow-book.epub"}`)
	if code != 202 {
		t.Fatalf("POST /download = %d %v", code, m)
	}
	id := m["jobId"].(string)

	// Wait for the job to actually be running (mid-transfer) before
	// cancelling, so this exercises the running-job cancel path rather
	// than the queued one.
	runningDeadline := time.Now().Add(5 * time.Second)
	for {
		code, dj := apiReq(t, ts, "GET", "/download/"+id, "")
		if code != 200 {
			t.Fatalf("GET /download/%s = %d %v", id, code, dj)
		}
		if dj["status"] == "running" {
			break
		}
		if time.Now().After(runningDeadline) {
			t.Fatalf("download job never reached running: %v", dj)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Cancel returns immediately; for a running job the worker hasn't
	// necessarily finalized it yet (that happens asynchronously once it
	// observes jobCtx.Done()), so the response can still show "running" —
	// the poll loop below is what confirms the eventual "cancelled" state.
	code, cm := apiReq(t, ts, "DELETE", "/download/"+id, "")
	if code != 200 {
		t.Fatalf("DELETE /download/%s = %d %v", id, code, cm)
	}

	// Poll until the worker finalizes it (FinishCancelled), bounded well
	// under the DownloadJobTimeout so a regression here fails fast.
	cancelDeadline := time.Now().Add(5 * time.Second)
	var final map[string]interface{}
	for time.Now().Before(cancelDeadline) {
		code, dj := apiReq(t, ts, "GET", "/download/"+id, "")
		if code != 200 {
			t.Fatalf("GET /download/%s = %d %v", id, code, dj)
		}
		if dj["status"] == "cancelled" {
			final = dj
			break
		}
		if dj["status"] == "complete" || dj["status"] == "error" {
			t.Fatalf("download job = %v, want cancelled", dj)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if final == nil {
		t.Fatalf("download job did not reach cancelled within 5s")
	}

	// The queue slot must be freed: health reports the download worker
	// idle, not still "running" the cancelled job.
	code, health := apiReq(t, ts, "GET", "/health", "")
	if code != 200 {
		t.Fatalf("GET /health = %d %v", code, health)
	}
	dl, ok := health["download"].(map[string]interface{})
	if !ok || dl["running"] != false {
		t.Errorf("expected download queue freed after cancel, health = %v", health)
	}
}

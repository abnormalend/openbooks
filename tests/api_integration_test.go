//go:build integration

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			for _, line := range strings.Split(string(buf[:n]), "\n") {
				switch {
				case strings.Contains(line, "@search"):
					fmt.Fprintf(conn, ":search!u@h NOTICE tester :Your search has been accepted\r\n")
					fmt.Fprintf(conn, ":search!u@h PRIVMSG tester :DCC SEND Search_results_for__gatsby.txt.zip 2130706433 %s %d\r\n", resultsPort, resultsSize)
				case strings.HasPrefix(line, "PRIVMSG #ebooks :!"):
					fmt.Fprintf(conn, ":DV8!u@h PRIVMSG tester :DCC SEND great-gatsby.epub 2130706433 %s %d\r\n", bookPort, bookSize)
				}
			}
		}
	}()
	return l.Addr().String(), func() { l.Close(); wg.Wait() }
}

func apiReq(t *testing.T, ts *httptest.Server, method, path, body string) (int, map[string]interface{}) {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+"/api"+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer integration-token")
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]interface{}
	json.NewDecoder(res.Body).Decode(&m)
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
	h := server.NewHandler(ctx, server.Config{
		Basepath:           "/",
		DownloadDir:        dir,
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
	ts := httptest.NewServer(h)
	defer ts.Close()

	// Unauthenticated → 401
	res, _ := http.Post(ts.URL+"/api/search", "application/json", strings.NewReader(`{"query":"x"}`))
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
	if wsRes, _ := http.Get(ts.URL + "/ws"); wsRes.StatusCode != 409 {
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
	got, err := os.ReadFile(filepath.Join(dir, "books", "great-gatsby.epub"))
	if err != nil {
		t.Fatalf("book not on disk: %v", err)
	}
	if !bytes.Equal(got, book) {
		t.Errorf("book bytes differ: %d vs %d", len(got), len(book))
	}

	// Health reflects a connected, idle API session
	code, h2 := apiReq(t, ts, "GET", "/health", "")
	if code != 200 || h2["ircConnected"] != true {
		t.Errorf("health = %d %v", code, h2)
	}

	// Jobs list has both
	_, jl := apiReq(t, ts, "GET", "/jobs", "")
	if len(jl["jobs"].([]interface{})) != 2 {
		t.Errorf("jobs = %v", jl)
	}
}

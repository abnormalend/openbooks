// server/api/session_test.go
package api

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evan-buss/openbooks/core"
	"github.com/evan-buss/openbooks/irc"
)

// fakeIRC accepts one connection and returns it plus the listener address.
// The test drives the server side by writing IRC lines into `srv`.
// fakeIRC accepts connections in a loop (so tests can Connect/Disconnect/
// Connect again against the same listener) and pushes each one onto
// accepted, along with the listener address.
func fakeIRC(t *testing.T) (addr string, accepted <-chan net.Conn, stop func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			ch <- c
		}
	}()
	return l.Addr().String(), ch, func() { l.Close() }
}

func newTestSession(addr string, onServers func(core.IrcServers)) *Session {
	s := NewSession(SessionConfig{
		Nick: "tester", UserAgent: "OpenBooks test", Server: addr, SearchBot: "search",
	}, log.New(io.Discard, "", 0), onServers)
	// Skip core.Join's 2s sleep: connect + JOIN directly.
	s.join = func(c *irc.Conn, address string, tls bool) error {
		if err := c.Connect(address, tls); err != nil {
			return err
		}
		c.JoinChannel("ebooks")
		return nil
	}
	return s
}

func waitEvent(t *testing.T, ch <-chan Event, want EventKind) Event {
	t.Helper()
	select {
	case ev := <-ch:
		if ev.Kind != want {
			t.Fatalf("event kind = %v, want %v (text %q)", ev.Kind, want, ev.Text)
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for event %v", want)
	}
	return Event{}
}

func TestSessionConnectSearchAndRouteEvents(t *testing.T) {
	addr, accepted, stop := fakeIRC(t)
	defer stop()

	var serversMu sync.Mutex
	var gotServers core.IrcServers
	s := newTestSession(addr, func(sv core.IrcServers) {
		serversMu.Lock()
		gotServers = sv
		serversMu.Unlock()
	})
	if err := s.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !s.Connected() {
		t.Fatal("Connected() should be true")
	}
	// Second Connect is a no-op.
	if err := s.Connect(); err != nil {
		t.Fatalf("second Connect: %v", err)
	}

	srv := <-accepted
	defer srv.Close()
	reader := bufio.NewReader(srv)

	// Client should have sent USER/NICK/JOIN.
	var lines []string
	for i := 0; i < 3; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, strings.TrimSpace(line))
	}
	if !strings.HasPrefix(lines[0], "USER tester") || lines[1] != "NICK tester" || lines[2] != "JOIN #ebooks" {
		t.Fatalf("handshake lines = %q", lines)
	}

	s.SearchBook("the great gatsby")
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading search line: %v", err)
	}
	if strings.TrimSpace(line) != "PRIVMSG #ebooks :@search the great gatsby" {
		t.Errorf("search line = %q", line)
	}

	s.DownloadBook("!DV8 Some Book.epub")
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading download line: %v", err)
	}
	if strings.TrimSpace(line) != "PRIVMSG #ebooks :!DV8 Some Book.epub" {
		t.Errorf("download line = %q", line)
	}

	// Server-side events fan out to the right channels.
	fmt.Fprint(srv, "353 ~DV8 ~Horla +user1\r\n")
	fmt.Fprint(srv, "end 366\r\n")
	fmt.Fprint(srv, ":search!x@y NOTICE tester :Your search for foo has been accepted\r\n")
	fmt.Fprint(srv, ":search!x@y NOTICE tester :Search returned 27 matches\r\n")
	fmt.Fprint(srv, ":search!x@y PRIVMSG tester :DCC SEND Search_results_for__foo.txt.zip 2130706433 6668 10\r\n")
	fmt.Fprint(srv, ":search!x@y NOTICE tester :Sorry, nothing found\r\n")
	fmt.Fprint(srv, ":DV8!x@y PRIVMSG tester :DCC SEND book.epub 2130706433 6669 10\r\n")
	fmt.Fprint(srv, ":DV8!x@y NOTICE tester :try another server\r\n")

	// core.StartReader dispatches handlers via `go invoke(...)`, so the four
	// search-queue events (SearchAccepted, MatchesFound, SearchResult,
	// NoResults) are not guaranteed to land on the channel in send order.
	// Collect them and assert membership rather than order.
	wantSearchKinds := map[EventKind]bool{
		EvSearchAccepted: true,
		EvMatchesFound:   true,
		EvSearchResult:   true,
		EvNoResults:      true,
	}
	gotSearch := make(map[EventKind]Event)
	for i := 0; i < len(wantSearchKinds); i++ {
		select {
		case ev := <-s.SearchEvents():
			gotSearch[ev.Kind] = ev
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for search event %d of %d (got so far: %+v)", i+1, len(wantSearchKinds), gotSearch)
		}
	}
	for kind := range wantSearchKinds {
		if _, ok := gotSearch[kind]; !ok {
			t.Errorf("missing search event kind %v; got %+v", kind, gotSearch)
		}
	}
	if ev, ok := gotSearch[EvSearchResult]; ok && !strings.Contains(ev.Text, "DCC SEND Search_results") {
		t.Errorf("SearchResult text = %q", ev.Text)
	}
	// Same non-determinism as above applies to the two download events
	// (BookResult, BadServer): both are dispatched via `go invoke(...)`
	// from core.StartReader, so their arrival order is not guaranteed.
	wantDownloadKinds := map[EventKind]bool{
		EvBookResult: true,
		EvBadServer:  true,
	}
	gotDownload := make(map[EventKind]Event)
	for i := 0; i < len(wantDownloadKinds); i++ {
		select {
		case ev := <-s.DownloadEvents():
			gotDownload[ev.Kind] = ev
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for download event %d of %d (got so far: %+v)", i+1, len(wantDownloadKinds), gotDownload)
		}
	}
	for kind := range wantDownloadKinds {
		if _, ok := gotDownload[kind]; !ok {
			t.Errorf("missing download event kind %v; got %+v", kind, gotDownload)
		}
	}

	// ServerList callback fired with parsed elevated users.
	deadline := time.Now().Add(time.Second)
	for {
		serversMu.Lock()
		n := len(gotServers.ElevatedUsers)
		serversMu.Unlock()
		if n != 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	serversMu.Lock()
	finalServers := gotServers
	serversMu.Unlock()
	// The fake "353" line contributes 3 elevated names (~DV8, ~Horla,
	// +user1): core.ParseServers splits on spaces and treats any token
	// beginning with an elevation prefix (~&@%+) as elevated, regardless
	// of position, so all three qualify.
	if len(finalServers.ElevatedUsers) != 3 {
		t.Errorf("servers = %+v", finalServers)
	}

	s.Disconnect()
	if s.Connected() {
		t.Error("Connected() should be false after Disconnect")
	}

	quitLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading QUIT line: %v", err)
	}
	if strings.TrimSpace(quitLine) != "QUIT :Goodbye" {
		t.Errorf("quit line = %q", quitLine)
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Errorf("expected EOF after QUIT, got err = %v", err)
	}

	// A caller-initiated Disconnect must not also emit EvDisconnected;
	// that event is reserved for the reader loop exiting on its own.
	select {
	case ev := <-s.SearchEvents():
		t.Errorf("unexpected search event after Disconnect: %+v", ev)
	case ev := <-s.DownloadEvents():
		t.Errorf("unexpected download event after Disconnect: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSessionRemoteCloseEmitsDisconnected(t *testing.T) {
	addr, accepted, stop := fakeIRC(t)
	defer stop()
	s := newTestSession(addr, nil)
	if err := s.Connect(); err != nil {
		t.Fatal(err)
	}
	srv := <-accepted
	srv.Close() // server drops us

	waitEvent(t, s.SearchEvents(), EvDisconnected)
	waitEvent(t, s.DownloadEvents(), EvDisconnected)
	deadline := time.Now().Add(time.Second)
	for s.Connected() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.Connected() {
		t.Error("session should be marked disconnected after remote close")
	}
}

func TestSessionConnectError(t *testing.T) {
	s := newTestSession("127.0.0.1:1", nil) // nothing listening
	if err := s.Connect(); err == nil {
		t.Fatal("expected connect error")
	}
	if s.Connected() {
		t.Error("should not be connected")
	}
}

func TestSessionReconnectAfterDisconnect(t *testing.T) {
	addr, accepted, stop := fakeIRC(t)
	defer stop()
	s := newTestSession(addr, nil)

	if err := s.Connect(); err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	first := <-accepted
	defer first.Close()
	if !s.Connected() {
		t.Fatal("Connected() should be true after first Connect")
	}

	s.Disconnect()
	if s.Connected() {
		t.Fatal("Connected() should be false after Disconnect")
	}

	if err := s.Connect(); err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	second := <-accepted
	defer second.Close()
	if !s.Connected() {
		t.Fatal("Connected() should be true after reconnect")
	}

	// Neither the manual Disconnect nor the reconnect should have emitted
	// EvDisconnected on either channel.
	select {
	case ev := <-s.SearchEvents():
		t.Errorf("unexpected search event: %+v", ev)
	case ev := <-s.DownloadEvents():
		t.Errorf("unexpected download event: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSessionLogDirWritesFile(t *testing.T) {
	addr, accepted, stop := fakeIRC(t)
	defer stop()

	dir := t.TempDir()
	s := NewSession(SessionConfig{
		Nick: "tester", UserAgent: "OpenBooks test", Server: addr, SearchBot: "search", LogDir: dir,
	}, log.New(io.Discard, "", 0), nil)
	s.join = func(c *irc.Conn, address string, tls bool) error {
		if err := c.Connect(address, tls); err != nil {
			return err
		}
		c.JoinChannel("ebooks")
		return nil
	}

	if err := s.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	srv := <-accepted
	defer srv.Close()

	fmt.Fprint(srv, ":search!x@y NOTICE tester :hello there\r\n")

	logsDir := filepath.Join(dir, "logs")
	deadline := time.Now().Add(2 * time.Second)
	var entries []os.DirEntry
	for {
		var err error
		entries, err = os.ReadDir(logsDir)
		if err == nil && len(entries) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for log file in %s (err: %v)", logsDir, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(entries) != 1 {
		t.Errorf("expected exactly one log file in %s, got %d", logsDir, len(entries))
	}

	s.Disconnect()
	if s.logFile != nil {
		t.Error("logFile should be nil after Disconnect")
	}
}

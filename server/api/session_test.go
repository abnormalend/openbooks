// server/api/session_test.go
package api

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evan-buss/openbooks/core"
	"github.com/evan-buss/openbooks/irc"
)

// fakeIRC accepts one connection and returns it plus the listener address.
// The test drives the server side by writing IRC lines into `srv`.
func fakeIRC(t *testing.T) (addr string, accepted <-chan net.Conn, stop func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan net.Conn, 1)
	go func() {
		c, err := l.Accept()
		if err == nil {
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
	line, _ := reader.ReadString('\n')
	if strings.TrimSpace(line) != "PRIVMSG #ebooks :@search the great gatsby" {
		t.Errorf("search line = %q", line)
	}

	s.DownloadBook("!DV8 Some Book.epub")
	line, _ = reader.ReadString('\n')
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

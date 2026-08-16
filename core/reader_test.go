package core

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evan-buss/openbooks/irc"
)

// readerConn satisfies net.Conn with canned read input.
type readerConn struct {
	in []byte
}

func (r *readerConn) Read(p []byte) (int, error) {
	if len(r.in) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.in)
	r.in = r.in[n:]
	return n, nil
}

func (r *readerConn) Write(p []byte) (int, error)      { return len(p), nil }
func (r *readerConn) Close() error                     { return nil }
func (r *readerConn) LocalAddr() net.Addr              { return nil }
func (r *readerConn) RemoteAddr() net.Addr             { return nil }
func (r *readerConn) SetDeadline(time.Time) error      { return nil }
func (r *readerConn) SetReadDeadline(time.Time) error  { return nil }
func (r *readerConn) SetWriteDeadline(time.Time) error { return nil }

// recorder collects events from concurrent handlers without channel-close
// races. StartReader dispatches handlers as goroutines, so naive
// chan + close patterns fail under -race.
type recorder struct {
	mu   sync.Mutex
	seen []event
}

func (r *recorder) record(e event) func(string) {
	return func(string) {
		r.mu.Lock()
		r.seen = append(r.seen, e)
		r.mu.Unlock()
	}
}

func (r *recorder) waitFor(t *testing.T, want int, timeout time.Duration) []event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		n := len(r.seen)
		r.mu.Unlock()
		if n >= want {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]event, len(r.seen))
	copy(out, r.seen)
	return out
}

func runReader(t *testing.T, lines string, handler EventHandler) {
	t.Helper()
	conn := &irc.Conn{Conn: &readerConn{in: []byte(lines)}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		StartReader(ctx, conn, handler)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StartReader did not return on EOF within 2s")
	}
}

func TestStartReaderClassifiesEvents(t *testing.T) {
	lines := strings.Join([]string{
		":server.example PING :server.example",
		":Bot!u@h PRIVMSG evan :DCC SEND SearchOok_results_for__gatsby.txt.zip 2130706433 6668 1184",
		":Bot!u@h PRIVMSG evan :DCC SEND book.epub 2130706433 6669 358887",
		":server NOTICE evan :Sorry, no matches",
		":server NOTICE evan :try another server",
		":server NOTICE evan :Search has been accepted",
		":server NOTICE evan :returned 27 matches",
		":bot PRIVMSG evan :\x01VERSION\x01",
		":353 ~admin +user1 ~user2",
		":366 end of NAMES",
	}, "\r\n") + "\r\n"

	r := &recorder{}
	handler := EventHandler{
		Ping:           r.record(Ping),
		SearchResult:   r.record(SearchResult),
		BookResult:     r.record(BookResult),
		NoResults:      r.record(NoResults),
		BadServer:      r.record(BadServer),
		SearchAccepted: r.record(SearchAccepted),
		MatchesFound:   r.record(MatchesFound),
		Version:        r.record(Version),
		ServerList:     r.record(ServerList),
	}
	runReader(t, lines, handler)

	got := r.waitFor(t, 9, 2*time.Second)
	seen := map[event]int{}
	for _, ev := range got {
		seen[ev]++
	}
	want := []event{Ping, SearchResult, BookResult, NoResults, BadServer, SearchAccepted, MatchesFound, Version, ServerList}
	for _, ev := range want {
		if seen[ev] == 0 {
			t.Errorf("expected event %v at least once; full count map: %+v", ev, seen)
		}
	}
}

func TestStartReaderDistinguishesSearchFromBookDccSend(t *testing.T) {
	lines := strings.Join([]string{
		":Bot!u@h PRIVMSG evan :DCC SEND SearchOok_results_for__foo.txt.zip 1 1 1",
		":Bot!u@h PRIVMSG evan :DCC SEND great-gatsby.epub 1 1 1",
	}, "\r\n") + "\r\n"

	r := &recorder{}
	handler := EventHandler{
		SearchResult: r.record(SearchResult),
		BookResult:   r.record(BookResult),
	}
	runReader(t, lines, handler)

	got := r.waitFor(t, 2, 2*time.Second)
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d (%v)", len(got), got)
	}
	hasSR, hasBR := false, false
	for _, ev := range got {
		if ev == SearchResult {
			hasSR = true
		}
		if ev == BookResult {
			hasBR = true
		}
	}
	if !hasSR || !hasBR {
		t.Errorf("expected both SearchResult and BookResult, got %v", got)
	}
}

func TestStartReaderClassifiesQueuePosition(t *testing.T) {
	lines := strings.Join([]string{
		":Bot!u@h NOTICE evan :Added Fourth Wing to queueposition 5.",         // NOTICE form
		":Bot!u@h PRIVMSG evan :You are now in queue position 3 for the file", // PRIVMSG form
		":Bot!u@h NOTICE evan :QUEUEPOSITION 1",                               // case-insensitive
	}, "\r\n") + "\r\n"

	r := &recorder{}
	handler := EventHandler{
		QueuePosition: r.record(QueuePosition),
		NoResults:     r.record(NoResults),
		BadServer:     r.record(BadServer),
	}
	runReader(t, lines, handler)

	got := r.waitFor(t, 3, 2*time.Second)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3 QueuePosition: %+v", len(got), got)
	}
	for _, ev := range got {
		if ev != QueuePosition {
			t.Errorf("event = %v, want QueuePosition", ev)
		}
	}
}

func TestQueuePositionDoesNotStealOtherNotices(t *testing.T) {
	// A queue line must not swallow the existing notice classifications.
	lines := strings.Join([]string{
		":server NOTICE evan :Sorry, nothing found",
		":server NOTICE evan :try another server",
		":Bot!u@h PRIVMSG evan :DCC SEND book.epub 1 1 1", // still a BookResult, no 'queue'
	}, "\r\n") + "\r\n"
	r := &recorder{}
	handler := EventHandler{
		NoResults:     r.record(NoResults),
		BadServer:     r.record(BadServer),
		BookResult:    r.record(BookResult),
		QueuePosition: r.record(QueuePosition),
	}
	runReader(t, lines, handler)
	got := r.waitFor(t, 3, 2*time.Second)
	seen := map[event]int{}
	for _, e := range got {
		seen[e]++
	}
	if seen[NoResults] != 1 || seen[BadServer] != 1 || seen[BookResult] != 1 || seen[QueuePosition] != 0 {
		t.Errorf("misclassified: %+v", seen)
	}
}

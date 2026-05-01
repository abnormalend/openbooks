package server

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/evan-buss/openbooks/irc"
)

const testNick = "evan"

func TestIsUserRelevantKeepsTargetedPrivmsgAndNotice(t *testing.T) {
	cases := []string{
		":Search!u@h NOTICE evan :Your search has been accepted.",
		":Search!u@h NOTICE evan :returned 27 matches.",
		":Horla!u@h NOTICE evan :Added Foo - Bar.epub to queueposition 5.",
		":Horla!u@h NOTICE evan :Sending you the requested file: Foo - Bar.epub",
		":Horla!u@h PRIVMSG evan :\x01DCC SEND foo.epub 1 1 1\x01",
		":SearchBot!u@h PRIVMSG evan :\x01DCC SEND results.txt.zip 1 1 1\x01",
		":ChanServ!s@s NOTICE evan :[#ebooks] Welcome to #ebooks.",
	}
	for _, line := range cases {
		if !isUserRelevant(line, testNick) {
			t.Errorf("expected to KEEP: %q", line)
		}
	}
}

func TestIsUserRelevantDropsChannelAndProtocolNoise(t *testing.T) {
	cases := []string{
		// Channel chatter from other users
		":hell2!h@h PRIVMSG #ebooks :!Horla Rick Mofina - Foo.epub",
		":bot!b@h PRIVMSG #ebooks :@search results announcement",
		// Server numerics
		":hyrule.tx.us.irchighway.net 001 evan :Welcome",
		":hyrule.tx.us.irchighway.net 005 evan AWAYLEN=200 :are supported",
		":hyrule.tx.us.irchighway.net 372 evan :- MOTD line",
		":hyrule.tx.us.irchighway.net 376 evan :End of MOTD.",
		":hyrule.tx.us.irchighway.net 332 evan #ebooks :Topic text",
		// Pre-auth connection ceremony - target is the literal "Auth" string
		":hyrule.tx.us.irchighway.net NOTICE Auth :*** Looking up your hostname...",
		":hyrule.tx.us.irchighway.net NOTICE Auth :Welcome to irchighway!",
		// Join / part / quit / mode
		":evan!u@h JOIN :#ebooks",
		":evan!u@h MODE evan +x",
		":someone!u@h QUIT :Ping timeout",
		":someone!u@h PART #ebooks :leaving",
		// PING keepalive and NAMES list
		"PING :server.example",
		":server.tld 353 evan = #ebooks :@op +voice user",
		":server.tld 366 evan #ebooks :End of /NAMES list",
		// Empty / malformed
		"",
		"   ",
		"PING",
	}
	for _, line := range cases {
		if isUserRelevant(line, testNick) {
			t.Errorf("expected to DROP: %q", line)
		}
	}
}

func TestIsUserRelevantNickMatchIsCaseInsensitive(t *testing.T) {
	if !isUserRelevant(":bot!u@h NOTICE EVAN :hi", "evan") {
		t.Error("EVAN should match nick 'evan' (IRC nicks are case-insensitive)")
	}
	if !isUserRelevant(":bot!u@h PRIVMSG Evan :hi", "EVAN") {
		t.Error("Evan should match nick 'EVAN'")
	}
}

func TestIsUserRelevantEmptyNickFailsClosed(t *testing.T) {
	// If we don't yet know our own nick, nothing is "ours" - drop everything.
	if isUserRelevant(":bot!u@h NOTICE evan :hi", "") {
		t.Error("empty nick should drop everything (fail-closed)")
	}
}

// newTestClient returns a Client with an irc.Conn whose Username is set,
// which is what ircLogHandler reads to filter targeted lines.
func newTestClient(t *testing.T, sendBuf int) *Client {
	t.Helper()
	return &Client{
		send: make(chan interface{}, sendBuf),
		log:  log.New(io.Discard, "", 0),
		irc:  irc.New(testNick, "OpenBooks test"),
	}
}

func TestIrcLogHandlerForwardsTargetedLines(t *testing.T) {
	c := newTestClient(t, 4)
	c.ircLogHandler()(":Horla!u@h NOTICE evan :Added foo to queueposition 3.")

	select {
	case msg := <-c.send:
		r, ok := msg.(IrcLogResponse)
		if !ok {
			t.Fatalf("got %T, want IrcLogResponse", msg)
		}
		if r.MessageType != IRC_MESSAGE {
			t.Errorf("MessageType = %v, want IRC_MESSAGE", r.MessageType)
		}
		if r.Line != ":Horla!u@h NOTICE evan :Added foo to queueposition 3." {
			t.Errorf("Line = %q, want raw line preserved", r.Line)
		}
	case <-time.After(time.Second):
		t.Fatal("expected IrcLogResponse on c.send, got nothing")
	}
}

func TestIrcLogHandlerSkipsChannelAndProtocolNoise(t *testing.T) {
	c := newTestClient(t, 4)
	handler := c.ircLogHandler()

	noise := []string{
		"PING :server.example",
		":srv 353 evan = #ebooks :@op +voice user",
		":srv 366 evan #ebooks :End of /NAMES list",
		":srv NOTICE Auth :*** Looking up your hostname...",
		":bot!b@h PRIVMSG #ebooks :channel chatter",
		":someone!u@h JOIN :#ebooks",
		":someone!u@h QUIT :Ping timeout",
	}
	for _, line := range noise {
		handler(line)
	}

	select {
	case msg := <-c.send:
		t.Errorf("expected all noise to be filtered, got %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// Good - nothing arrived.
	}
}

func TestIrcLogHandlerDropsWhenSendBufferFull(t *testing.T) {
	// Buffer of 1 to force the second send into the default branch.
	c := newTestClient(t, 1)
	handler := c.ircLogHandler()

	handler(":srv!u@h NOTICE evan :first")  // fills the buffer
	handler(":srv!u@h NOTICE evan :second") // must be dropped, not block

	// Drain what's there. Only the first should be present.
	got := 0
	for {
		select {
		case <-c.send:
			got++
		case <-time.After(20 * time.Millisecond):
			if got != 1 {
				t.Errorf("got %d items in send buffer, want 1 (second dropped)", got)
			}
			return
		}
	}
}

// startMockDccPayload boots a one-shot TCP listener that, on Accept,
// writes payload then closes. Returns the bound port and a stop fn.
func startMockDccPayload(t *testing.T, payload []byte) (port string, stop func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
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
		conn.Write(payload)
	}()
	return fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port), func() {
		l.Close()
		wg.Wait()
	}
}

func makeSearchResultsZip(t *testing.T, contents string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("results.txt")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write([]byte(contents)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// Regression: searchResultHandler must NOT depend on its downloadDir
// argument existing or being writable. Search-results zips are ephemeral
// and route through os.TempDir(); a missing/bogus downloadDir should not
// stop a search from succeeding.
func TestSearchResultHandlerIgnoresDownloadDir(t *testing.T) {
	const sample = `Search results from SearchBot
!DV8 F. Scott Fitzgerald - The Great Gatsby (Epub).rar  ::INFO:: 394.7KB
!Horla F Scott Fitzgerald - The Great Gatsby (retail) (epub).epub
`
	payload := makeSearchResultsZip(t, sample)
	port, stop := startMockDccPayload(t, payload)
	defer stop()

	c := newTestClient(t, 4)
	handler := c.searchResultHandler("/definitely/does/not/exist/at/all")

	// 2130706433 = 127.0.0.1
	dccText := fmt.Sprintf(
		":SearchBot!u@h PRIVMSG evan :\x01DCC SEND results.txt.zip 2130706433 %s %d\x01",
		port, len(payload),
	)
	handler(dccText)

	select {
	case msg := <-c.send:
		sr, ok := msg.(SearchResponse)
		if !ok {
			t.Fatalf("got %T (%+v), want SearchResponse - handler likely fell back to downloadDir", msg, msg)
		}
		if len(sr.Books) < 1 {
			t.Errorf("expected at least 1 book in SearchResponse, got %d", len(sr.Books))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no SearchResponse within 2s")
	}
}

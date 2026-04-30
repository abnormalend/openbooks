package server

import (
	"io"
	"log"
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

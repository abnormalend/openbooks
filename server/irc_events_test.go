package server

import (
	"io"
	"log"
	"testing"
	"time"
)

func TestIsProtocolNoiseFiltersPing(t *testing.T) {
	if !isProtocolNoise("PING :server.example") {
		t.Error("PING server keepalive should be filtered")
	}
	// Lines from a server-prefixed PING (rare but legal).
	if !isProtocolNoise(":server.example PING :hello") {
		t.Error(":prefix PING should be filtered")
	}
}

func TestIsProtocolNoiseFiltersNames(t *testing.T) {
	if !isProtocolNoise(":server.tld 353 user = #ebooks :@op +voice user") {
		t.Error("353 NAMES list reply should be filtered")
	}
	if !isProtocolNoise(":server.tld 366 user #ebooks :End of /NAMES list") {
		t.Error("366 end-of-NAMES should be filtered")
	}
}

func TestIsProtocolNoiseAllowsPrivmsgAndNotice(t *testing.T) {
	cases := []string{
		":SearchBot!u@h NOTICE evan :You are queued at position 3",
		":bot!u@h PRIVMSG #ebooks :Search currently full",
		":bot!u@h PRIVMSG evan :DCC SEND book.epub 1 1 1",
	}
	for _, line := range cases {
		if isProtocolNoise(line) {
			t.Errorf("expected line NOT to be filtered: %q", line)
		}
	}
}

func TestIsProtocolNoiseEmptyAndMalformed(t *testing.T) {
	if isProtocolNoise("") {
		t.Error("empty line should not be flagged as noise (defensive default)")
	}
	if isProtocolNoise("   ") {
		t.Error("whitespace-only line should not be flagged as noise")
	}
}

func TestIrcLogHandlerForwardsValidLines(t *testing.T) {
	c := &Client{
		send: make(chan interface{}, 4),
		log:  log.New(io.Discard, "", 0),
	}
	c.ircLogHandler()(":SearchBot!u@h NOTICE evan :Queued at position 3")

	select {
	case msg := <-c.send:
		r, ok := msg.(IrcLogResponse)
		if !ok {
			t.Fatalf("got %T, want IrcLogResponse", msg)
		}
		if r.MessageType != IRC_MESSAGE {
			t.Errorf("MessageType = %v, want IRC_MESSAGE", r.MessageType)
		}
		if r.Line == "" {
			t.Error("Line should be populated")
		}
	case <-time.After(time.Second):
		t.Fatal("expected IrcLogResponse on c.send, got nothing")
	}
}

func TestIrcLogHandlerSkipsProtocolNoise(t *testing.T) {
	c := &Client{
		send: make(chan interface{}, 4),
		log:  log.New(io.Discard, "", 0),
	}
	handler := c.ircLogHandler()

	handler("PING :server.example")
	handler(":srv 353 user = #ebooks :@op +voice user")
	handler(":srv 366 user #ebooks :End of /NAMES list")

	select {
	case msg := <-c.send:
		t.Errorf("expected protocol noise to be filtered, got %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// Good - nothing arrived.
	}
}

func TestIrcLogHandlerDropsWhenSendBufferFull(t *testing.T) {
	// Buffer of 1 to force the second send into the default branch.
	c := &Client{
		send: make(chan interface{}, 1),
		log:  log.New(io.Discard, "", 0),
	}
	handler := c.ircLogHandler()

	handler(":srv NOTICE evan :first") // fills the buffer
	handler(":srv NOTICE evan :second") // must be dropped, not block

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

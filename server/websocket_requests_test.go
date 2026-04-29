package server

import (
	"encoding/json"
	"io"
	"log"
	"testing"
	"time"
)

func TestRouteMessageRateLimitsSearch(t *testing.T) {
	s := New(Config{SearchTimeout: 10 * time.Second})
	s.lastSearch = time.Now() // search just happened, next is rate-limited

	c := &Client{
		send: make(chan interface{}, 4),
		log:  log.New(io.Discard, "", 0),
	}
	req := Request{MessageType: SEARCH, Payload: json.RawMessage(`{"query":"foo"}`)}
	s.routeMessage(req, c)

	select {
	case msg := <-c.send:
		sr, ok := msg.(StatusResponse)
		if !ok {
			t.Fatalf("got %T, want StatusResponse", msg)
		}
		if sr.MessageType != RATELIMIT {
			t.Errorf("MessageType = %v, want RATELIMIT", sr.MessageType)
		}
		if sr.NotificationType != WARNING {
			t.Errorf("NotificationType = %v, want WARNING", sr.NotificationType)
		}
	case <-time.After(time.Second):
		t.Fatal("no response received within 1s")
	}
}

func TestRouteMessageMalformedPayload(t *testing.T) {
	// Set lastSearch to now so the SEARCH dispatch hits the rate-limit
	// branch (which doesn't touch IRC) instead of trying to write to a
	// real connection.
	s := New(Config{SearchTimeout: 10 * time.Second})
	s.lastSearch = time.Now()

	c := &Client{
		send: make(chan interface{}, 4),
		log:  log.New(io.Discard, "", 0),
	}
	req := Request{MessageType: SEARCH, Payload: json.RawMessage(`{not valid json`)}
	s.routeMessage(req, c)

	// First message on the channel should be the unmarshal-error response.
	select {
	case msg := <-c.send:
		sr, ok := msg.(StatusResponse)
		if !ok {
			t.Fatalf("got %T, want StatusResponse", msg)
		}
		if sr.NotificationType != DANGER {
			t.Errorf("NotificationType = %v, want DANGER", sr.NotificationType)
		}
	case <-time.After(time.Second):
		t.Fatal("no error response received within 1s")
	}
}

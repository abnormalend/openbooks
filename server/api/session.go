// server/api/session.go
package api

import (
	"context"
	"log"
	"sync"

	"github.com/evan-buss/openbooks/core"
	"github.com/evan-buss/openbooks/irc"
	"github.com/evan-buss/openbooks/util"
)

// EventKind classifies IRC events the workers care about.
type EventKind int

const (
	EvSearchAccepted EventKind = iota
	EvMatchesFound
	EvSearchResult
	EvNoResults
	EvBookResult
	EvBadServer
	// EvDisconnected is pushed onto both channels when the IRC reader exits.
	EvDisconnected
)

// Event is one IRC event routed to a worker. Text is the raw IRC line
// (for SearchResult/BookResult it contains the DCC SEND string).
type Event struct {
	Kind EventKind
	Text string
}

type SessionConfig struct {
	Nick      string
	UserAgent string
	Server    string
	TLS       bool
	SearchBot string
	// LogDir, when non-empty, enables raw IRC logging into <LogDir>/logs
	// exactly like the browser client's --log behaviour.
	LogDir string
}

// Session is the API's single headless IRC connection. It is created
// disconnected; workers call Connect lazily and the idle watcher calls
// Disconnect. Events from core.StartReader are fanned out onto two
// buffered channels, one per worker.
type Session struct {
	cfg          SessionConfig
	log          *log.Logger
	onServerList func(core.IrcServers)
	// join is core.Join in production; tests inject a sleep-free variant.
	join func(*irc.Conn, string, bool) error

	mu        sync.Mutex
	conn      *irc.Conn
	cancel    context.CancelFunc
	connected bool

	searchEv   chan Event
	downloadEv chan Event
}

const eventBuffer = 32

func NewSession(cfg SessionConfig, logger *log.Logger, onServerList func(core.IrcServers)) *Session {
	return &Session{
		cfg:          cfg,
		log:          logger,
		onServerList: onServerList,
		join:         core.Join,
		searchEv:     make(chan Event, eventBuffer),
		downloadEv:   make(chan Event, eventBuffer),
	}
}

func (s *Session) SearchEvents() <-chan Event   { return s.searchEv }
func (s *Session) DownloadEvents() <-chan Event { return s.downloadEv }

func (s *Session) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

// Connect dials IRC and starts the reader. No-op when already connected.
func (s *Session) Connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.connected {
		return nil
	}
	conn := irc.New(s.cfg.Nick, s.cfg.UserAgent)
	if err := s.join(conn, s.cfg.Server, s.cfg.TLS); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.conn, s.cancel, s.connected = conn, cancel, true
	s.log.Printf("API session connected to %s as %s", s.cfg.Server, s.cfg.Nick)

	handler := s.handlers(conn)
	go func() {
		core.StartReader(ctx, conn, handler)
		s.readerExited(conn)
	}()
	return nil
}

// Disconnect closes the IRC connection if open. Safe to call when idle.
func (s *Session) Disconnect() {
	s.mu.Lock()
	if !s.connected {
		s.mu.Unlock()
		return
	}
	conn := s.conn
	s.connected = false
	s.conn = nil
	s.cancel()
	s.mu.Unlock()
	s.log.Println("API session disconnected")
	conn.Disconnect()
}

// readerExited runs when core.StartReader returns (socket closed by the
// remote or by Disconnect). Only a still-current conn transitions state
// and notifies workers; a conn we already tore down is ignored.
func (s *Session) readerExited(conn *irc.Conn) {
	s.mu.Lock()
	if s.conn != conn {
		s.mu.Unlock()
		return
	}
	s.connected = false
	s.conn = nil
	s.cancel()
	s.mu.Unlock()
	s.log.Println("API session lost IRC connection")
	conn.Disconnect()
	s.emit(s.searchEv, Event{Kind: EvDisconnected})
	s.emit(s.downloadEv, Event{Kind: EvDisconnected})
}

func (s *Session) SearchBook(query string) {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn != nil {
		core.SearchBook(conn, s.cfg.SearchBot, query)
	}
}

func (s *Session) DownloadBook(book string) {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn != nil {
		core.DownloadBook(conn, book)
	}
}

// emit never blocks: core.StartReader dispatches most handlers on their
// own goroutine, but a stuck send would still leak goroutines. Drop on full.
func (s *Session) emit(ch chan Event, ev Event) {
	select {
	case ch <- ev:
	default:
		s.log.Printf("API session event buffer full, dropping event %d", ev.Kind)
	}
}

func (s *Session) route(ch chan Event, kind EventKind) core.HandlerFunc {
	return func(text string) { s.emit(ch, Event{Kind: kind, Text: text}) }
}

// handlers mirrors server.NewIrcEventHandler but routes into channels
// instead of a websocket.
func (s *Session) handlers(conn *irc.Conn) core.EventHandler {
	h := core.EventHandler{
		core.SearchAccepted: s.route(s.searchEv, EvSearchAccepted),
		core.MatchesFound:   s.route(s.searchEv, EvMatchesFound),
		core.SearchResult:   s.route(s.searchEv, EvSearchResult),
		core.NoResults:      s.route(s.searchEv, EvNoResults),
		core.BookResult:     s.route(s.downloadEv, EvBookResult),
		core.BadServer:      s.route(s.downloadEv, EvBadServer),
		core.Ping:           func(text string) { conn.Pong(text) },
		core.Version:        func(line string) { core.SendVersionInfo(conn, line, s.cfg.UserAgent) },
		core.ServerList: func(text string) {
			if s.onServerList != nil {
				s.onServerList(core.ParseServers(text))
			}
		},
	}
	if s.cfg.LogDir != "" {
		logger, _, err := util.CreateLogFile(s.cfg.Nick+"-api", s.cfg.LogDir)
		if err != nil {
			s.log.Println(err)
		} else {
			h[core.Message] = func(text string) { logger.Println(text) }
		}
	}
	return h
}

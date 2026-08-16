// server/api/session.go
package api

import (
	"context"
	"errors"
	"io"
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
	EvQueuePosition
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

	// connectMu serializes Connect calls so a second concurrent caller
	// blocks behind the first (rather than racing to dial twice), then
	// observes connected == true under mu and returns nil.
	connectMu sync.Mutex

	mu        sync.Mutex
	conn      *irc.Conn
	cancel    context.CancelFunc
	connected bool
	// logFile is the open handle behind the optional per-connection IRC
	// log (see SessionConfig.LogDir). Closed and nilled out whenever the
	// connection tears down, from either Disconnect or readerExited.
	logFile io.Closer

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
// Concurrent callers are serialized on connectMu: a second caller blocks
// until the first finishes dialing, then observes connected == true and
// returns nil rather than racing to open a second TCP connection (which
// would otherwise leave one caller holding a *Session with a nil conn,
// so SearchBook/DownloadBook silently no-op and the job stalls to
// timeout). The dial/join and log file creation happen without holding
// s.mu so a slow or hanging network call doesn't block
// Connected/SearchBook/DownloadBook for the duration; connectMu is only
// ever held by Connect itself, never across a call into s.mu.
func (s *Session) Connect() error {
	s.connectMu.Lock()
	defer s.connectMu.Unlock()

	s.mu.Lock()
	if s.connected {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	conn := irc.New(s.cfg.Nick, s.cfg.UserAgent)
	joinErr := s.join(conn, s.cfg.Server, s.cfg.TLS)
	if joinErr != nil {
		return joinErr
	}

	var logger *log.Logger
	var logFile io.Closer
	if s.cfg.LogDir != "" {
		l, closer, err := util.CreateLogFile(s.cfg.Nick+"-api", s.cfg.LogDir)
		if err != nil {
			s.log.Println(err)
		} else {
			logger, logFile = l, closer
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.conn, s.cancel, s.connected, s.logFile = conn, cancel, true, logFile
	s.mu.Unlock()
	s.log.Printf("API session connected to %s as %s", s.cfg.Server, s.cfg.Nick)

	handler := s.handlers(conn, logger)
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
	logFile := s.logFile
	s.connected = false
	s.conn = nil
	s.cancel()
	s.cancel = nil
	s.logFile = nil
	s.mu.Unlock()
	s.log.Println("API session disconnected")
	conn.Disconnect()
	closeLogFile(s.log, logFile)
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
	logFile := s.logFile
	s.connected = false
	s.conn = nil
	s.cancel()
	s.cancel = nil
	s.logFile = nil
	s.mu.Unlock()
	s.log.Println("API session lost IRC connection")
	conn.Disconnect()
	closeLogFile(s.log, logFile)
	s.emit(s.searchEv, Event{Kind: EvDisconnected})
	s.emit(s.downloadEv, Event{Kind: EvDisconnected})
}

// closeLogFile closes f if non-nil, logging any close error. f is nil when
// LogDir was unset or CreateLogFile failed.
func closeLogFile(logger *log.Logger, f io.Closer) {
	if f == nil {
		return
	}
	if err := f.Close(); err != nil {
		logger.Println(err)
	}
}

// SearchBook sends the search request over the current IRC connection. It
// returns an error without sending anything if the session was disconnected
// between the caller's Connect and this call (e.g. a remote close raced the
// worker), so callers can fail the job fast instead of waiting out the full
// timeout for a reply that will never arrive.
func (s *Session) SearchBook(query string) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return errors.New("irc session not connected")
	}
	core.SearchBook(conn, s.cfg.SearchBot, query)
	return nil
}

// DownloadBook sends the download request over the current IRC connection.
// See SearchBook for why it returns an error instead of silently no-oping.
func (s *Session) DownloadBook(book string) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return errors.New("irc session not connected")
	}
	core.DownloadBook(conn, book)
	return nil
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
// instead of a websocket. It is pure: it only captures conn (for Ping/
// Version replies) and the already-opened logger (for raw IRC logging),
// neither of which requires s.mu.
func (s *Session) handlers(conn *irc.Conn, logger *log.Logger) core.EventHandler {
	h := core.EventHandler{
		core.SearchAccepted: s.route(s.searchEv, EvSearchAccepted),
		core.MatchesFound:   s.route(s.searchEv, EvMatchesFound),
		core.SearchResult:   s.route(s.searchEv, EvSearchResult),
		core.NoResults:      s.route(s.searchEv, EvNoResults),
		core.BookResult:     s.route(s.downloadEv, EvBookResult),
		core.BadServer:      s.route(s.downloadEv, EvBadServer),
		core.QueuePosition:  s.route(s.downloadEv, EvQueuePosition),
		core.Ping:           func(text string) { conn.Pong(text) },
		core.Version:        func(line string) { core.SendVersionInfo(conn, line, s.cfg.UserAgent) },
		core.ServerList: func(text string) {
			if s.onServerList != nil {
				s.onServerList(core.ParseServers(text))
			}
		},
	}
	if logger != nil {
		h[core.Message] = func(text string) { logger.Println(text) }
	}
	return h
}

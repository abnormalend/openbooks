package server

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/rs/cors"

	"github.com/evan-buss/openbooks/server/api"
)

type server struct {
	// Shared app configuration
	config *Config

	// Shared data
	repository *Repository

	// Registered clients.
	clients map[uuid.UUID]*Client

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	log *log.Logger

	// Rate limits searches across the browser client and the API worker.
	searchLimiter *api.SearchLimiter

	// REST API façade (mounted at <basepath>api).
	api *api.API

	// clientCount mirrors len(clients) without needing the hub goroutine;
	// read from other goroutines (serveWs, the API's BrowserConnected
	// check) that must not touch the clients map directly.
	clientCount atomic.Int32
}

// Config contains settings for server
type Config struct {
	Log         bool
	Port        string
	UserName    string
	Persist     bool
	DownloadDir string
	// LibrarySubdir is the subdirectory under DownloadDir where downloaded
	// books are stored and served from. Empty is a valid, intended value
	// meaning "the DownloadDir root" — not coerced to a default here (the
	// CLI flag's default is "books").
	LibrarySubdir           string
	Basepath                string
	Server                  string
	EnableTLS               bool
	SearchTimeout           time.Duration
	SearchBot               string
	DisableBrowserDownloads bool
	UserAgent               string

	// REST API (server/api). Empty APIToken disables the API (503s).
	APIToken           string
	APIIdleTimeout     time.Duration
	SearchJobTimeout   time.Duration
	DownloadJobTimeout time.Duration
	// Version is reported by /api/health.
	Version string
}

func New(config Config) *server {
	s := &server{
		repository:    NewRepository(),
		config:        &config,
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		clients:       make(map[uuid.UUID]*Client),
		log:           log.New(os.Stdout, "SERVER: ", log.LstdFlags|log.Lmsgprefix),
		searchLimiter: api.NewSearchLimiter(config.SearchTimeout),
	}
	logDir := ""
	if config.Log {
		logDir = config.DownloadDir
	}
	s.api = api.New(api.Config{
		Token:           config.APIToken,
		Version:         config.Version,
		BasePath:        config.Basepath,
		DownloadDir:     config.DownloadDir,
		LibrarySubdir:   config.LibrarySubdir,
		IdleTimeout:     config.APIIdleTimeout,
		SearchTimeout:   config.SearchJobTimeout,
		DownloadTimeout: config.DownloadJobTimeout,
		Session: api.SessionConfig{
			Nick:      config.UserName,
			UserAgent: config.UserAgent,
			Server:    config.Server,
			TLS:       config.EnableTLS,
			SearchBot: config.SearchBot,
			LogDir:    logDir,
		},
	}, api.Deps{
		Limiter:          s.searchLimiter,
		BrowserConnected: func() bool { return s.clientCount.Load() > 0 },
		Servers:          s.repository.Servers,
		OnServerList:     s.repository.SetServers,
		Log:              log.New(os.Stdout, "API: ", log.LstdFlags|log.Lmsgprefix),
	})
	return s
}

// NewHandler builds the full HTTP handler (SPA, websocket, legacy REST and
// the /api group mounted under config.Basepath) and starts the background
// goroutines bound to ctx. Start wraps it; tests drive it via httptest.
func NewHandler(ctx context.Context, config Config) http.Handler {
	createBooksDirectory(config)
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)

	corsConfig := cors.Options{
		AllowCredentials: true,
		AllowedOrigins:   []string{"http://127.0.0.1:5173"},
		AllowedHeaders:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "DELETE"},
	}
	router.Use(cors.New(corsConfig).Handler)

	server := New(config)
	routes := server.registerRoutes()

	go server.startClientHub(ctx)
	server.api.Start(ctx)
	router.Mount(config.Basepath, routes)

	server.log.Printf("Base Path: %s\n", config.Basepath)
	server.log.Printf("Download Directory: %s\n", config.DownloadDir)
	if config.APIToken == "" {
		server.log.Println("REST API disabled (no --api-token / OPENBOOKS_API_TOKEN)")
	} else {
		server.log.Printf("REST API enabled at %sapi/\n", config.Basepath)
	}
	return router
}

// Start instantiates the web server and blocks.
func Start(config Config) {
	ctx, cancel := context.WithCancel(context.Background())
	router := NewHandler(ctx, config)
	registerGracefulShutdown(cancel)

	log.Printf("SERVER: OpenBooks is listening on port %v", config.Port)
	log.Printf("SERVER: Open http://localhost:%v%s in your browser.", config.Port, config.Basepath)
	log.Fatal(http.ListenAndServe(":"+config.Port, router))
}

// The client hub is to be run in a goroutine and handles management of
// websocket client registrations.
func (server *server) startClientHub(ctx context.Context) {
	for {
		select {
		case client := <-server.register:
			server.clients[client.uuid] = client
			server.clientCount.Add(1)
		case client := <-server.unregister:
			if _, ok := server.clients[client.uuid]; ok {
				_, cancel := context.WithCancel(client.ctx)
				close(client.send)
				cancel()
				delete(server.clients, client.uuid)
				server.clientCount.Add(-1)
			}
		case <-ctx.Done():
			for _, client := range server.clients {
				_, cancel := context.WithCancel(client.ctx)
				close(client.send)
				cancel()
				delete(server.clients, client.uuid)
				server.clientCount.Add(-1)
			}
			return
		}
	}
}

func registerGracefulShutdown(cancel context.CancelFunc) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("SERVER: Graceful shutdown.")
		// Close the shutdown channel. Triggering all reader/writer WS handlers to close.
		cancel()
		time.Sleep(time.Second)
		os.Exit(0)
	}()
}

// libraryDir is the directory downloaded books are stored in and served
// from: DownloadDir joined with LibrarySubdir. An empty LibrarySubdir is a
// valid, intended configuration meaning "the DownloadDir root".
func (c Config) libraryDir() string {
	return filepath.Join(c.DownloadDir, c.LibrarySubdir)
}

func createBooksDirectory(config Config) {
	err := os.MkdirAll(config.libraryDir(), os.FileMode(0755))
	if err != nil {
		panic(err)
	}
}

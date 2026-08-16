package server

import (
	"sync"

	"github.com/evan-buss/openbooks/core"
)

type Repository struct {
	mu      sync.RWMutex
	servers core.IrcServers
}

func NewRepository() *Repository {
	return &Repository{servers: core.IrcServers{}}
}

// Servers returns the last known IRC server list.
func (r *Repository) Servers() core.IrcServers {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.servers
}

// SetServers replaces the IRC server list.
func (r *Repository) SetServers(s core.IrcServers) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.servers = s
}

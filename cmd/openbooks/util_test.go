package main

import (
	"testing"
	"time"

	"github.com/evan-buss/openbooks/server"
)

func TestSanitizePath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/", "/"},
		{"/openbooks", "/openbooks/"},
		{"/openbooks/", "/openbooks/"},
		{"//openbooks", "/openbooks/"},
		{"/openbooks//", "/openbooks/"},
		{"/a/b/c", "/a/b/c/"},
	}
	for _, c := range cases {
		if got := sanitizePath(c.in); got != c.want {
			t.Errorf("sanitizePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEnsureValidRateFloorsAtTen(t *testing.T) {
	cases := []struct {
		in   int
		want time.Duration
	}{
		{-1, 10 * time.Second},
		{0, 10 * time.Second},
		{5, 10 * time.Second},
		{10, 10 * time.Second},
		{30, 30 * time.Second},
		{60, 60 * time.Second},
	}
	for _, c := range cases {
		var cfg server.Config
		ensureValidRate(c.in, &cfg)
		if cfg.SearchTimeout != c.want {
			t.Errorf("ensureValidRate(%d).SearchTimeout = %v, want %v", c.in, cfg.SearchTimeout, c.want)
		}
	}
}

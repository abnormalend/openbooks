package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestRequireUserAcceptsValidCookie(t *testing.T) {
	s := New(Config{})
	called := false
	mw := s.requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/library", nil)
	req.AddCookie(&http.Cookie{Name: "OpenBooks", Value: uuid.New().String()})
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if !called {
		t.Error("next handler should be called when cookie is valid")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRequireUserRejectsMissingCookie(t *testing.T) {
	s := New(Config{})
	called := false
	mw := s.requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("GET", "/library", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if called {
		t.Error("next handler should NOT run without a cookie")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRequireUserRejectsMalformedUUID(t *testing.T) {
	s := New(Config{})
	called := false
	mw := s.requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("GET", "/library", nil)
	req.AddCookie(&http.Cookie{Name: "OpenBooks", Value: "not-a-uuid"})
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if called {
		t.Error("next handler should NOT run with a malformed uuid")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

package server

import (
	"strings"
	"testing"

	"github.com/evan-buss/openbooks/core"
)

func TestNewRateLimitResponseSecondsPlural(t *testing.T) {
	r := newRateLimitResponse(2.4)
	if r.MessageType != RATELIMIT {
		t.Errorf("MessageType = %v, want RATELIMIT", r.MessageType)
	}
	if r.NotificationType != WARNING {
		t.Errorf("NotificationType = %v, want WARNING", r.NotificationType)
	}
	if !strings.Contains(r.Detail, "2 seconds") {
		t.Errorf("Detail = %q, want it to contain '2 seconds'", r.Detail)
	}
}

func TestNewRateLimitResponseSecondSingular(t *testing.T) {
	r := newRateLimitResponse(0.6)
	if !strings.Contains(r.Detail, "1 second") {
		t.Errorf("Detail = %q, want '1 second'", r.Detail)
	}
	// Make sure we don't get "1 seconds"
	if strings.Contains(r.Detail, "1 seconds") {
		t.Errorf("Detail = %q, should not contain '1 seconds'", r.Detail)
	}
}

func TestNewSearchResponseSummary(t *testing.T) {
	books := []core.BookDetail{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	errs := []core.ParseError{{Line: "bad"}, {Line: "alsobad"}}
	r := newSearchResponse(books, errs)
	if r.MessageType != SEARCH {
		t.Errorf("MessageType = %v, want SEARCH", r.MessageType)
	}
	if !strings.Contains(r.Title, "3 Search Results") {
		t.Errorf("Title = %q, want '3 Search Results' substring", r.Title)
	}
	if !strings.Contains(r.Detail, "2 parsing errors") {
		t.Errorf("Detail = %q, want '2 parsing errors'", r.Detail)
	}
}

func TestNewSearchResponseSinglePluralization(t *testing.T) {
	r := newSearchResponse(nil, []core.ParseError{{Line: "bad"}})
	if !strings.Contains(r.Detail, "1 parsing error.") {
		t.Errorf("Detail = %q, want '1 parsing error.' (singular)", r.Detail)
	}
}

func TestNewDownloadResponseBrowserMode(t *testing.T) {
	r := newDownloadResponse("/abs/path/book.epub", false)
	if r.DownloadPath == "" {
		t.Error("expected DownloadPath set when browser downloads enabled")
	}
	if !strings.HasSuffix(r.DownloadPath, "library/book.epub") {
		t.Errorf("DownloadPath = %q, want suffix 'library/book.epub'", r.DownloadPath)
	}
	// Detail uses the basename only when browser downloads are enabled.
	if r.Detail != "book.epub" {
		t.Errorf("Detail = %q, want 'book.epub'", r.Detail)
	}
}

func TestNewDownloadResponseDisableBrowserMode(t *testing.T) {
	r := newDownloadResponse("/abs/path/book.epub", true)
	if r.DownloadPath != "" {
		t.Errorf("DownloadPath = %q, want empty when browser downloads disabled", r.DownloadPath)
	}
	// Server-side download mode shows the absolute path so users can find it.
	if r.Detail != "/abs/path/book.epub" {
		t.Errorf("Detail = %q, want full path", r.Detail)
	}
}

func TestNewStatusAndErrorResponses(t *testing.T) {
	s := newStatusResponse(SUCCESS, "ok")
	if s.MessageType != STATUS || s.NotificationType != SUCCESS || s.Title != "ok" {
		t.Errorf("status response wrong: %+v", s)
	}
	e := newErrorResponse("nope")
	if e.MessageType != STATUS || e.NotificationType != DANGER || e.Title != "nope" {
		t.Errorf("error response wrong: %+v", e)
	}
}

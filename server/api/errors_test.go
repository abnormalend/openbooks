// server/api/errors_test.go
package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteErrorShape(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, 409, "queue_full", "3 jobs already queued")

	if rec.Code != 409 {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body not JSON: %v: %s", err, rec.Body.String())
	}
	if got.Code != "queue_full" || got.Message != "3 jobs already queued" {
		t.Errorf("got %+v", got)
	}
}

func TestWriteJSONSetsHeaderAndStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, 202, map[string]string{"jobId": "abc"})
	if rec.Code != 202 {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("missing content-type")
	}
	if rec.Body.String() != "{\"jobId\":\"abc\"}\n" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

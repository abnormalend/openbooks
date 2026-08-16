// server/api/auth_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
}

func doAuth(token, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	RequireToken(token)(okHandler()).ServeHTTP(rec, req)
	return rec
}

func TestRequireTokenUnsetFailsClosed(t *testing.T) {
	rec := doAuth("", "Bearer anything")
	if rec.Code != 503 {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var e APIError
	json.Unmarshal(rec.Body.Bytes(), &e)
	if e.Code != "api_disabled" {
		t.Errorf("code = %q", e.Code)
	}
}

func TestRequireTokenMissingHeader(t *testing.T) {
	if rec := doAuth("s3cret", ""); rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRequireTokenWrongToken(t *testing.T) {
	if rec := doAuth("s3cret", "Bearer nope"); rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec := doAuth("s3cret", "Basic s3cret"); rec.Code != 401 {
		t.Fatalf("non-bearer scheme status = %d, want 401", rec.Code)
	}
}

func TestRequireTokenRightToken(t *testing.T) {
	if rec := doAuth("s3cret", "Bearer s3cret"); rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

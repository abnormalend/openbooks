// server/api/errors.go
// Package api implements the token-protected REST API that lets an
// external agent search IRC book servers and download results without
// the browser UI. See docs/superpowers/specs/2026-08-15-rest-api-design.md.
package api

import (
	"encoding/json"
	"net/http"
)

// APIError is the JSON body of every non-2xx response and the shape of a
// job's terminal error.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// writeJSON encodes v as the response body with the given status.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a structured {"code","message"} error body.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, APIError{Code: code, Message: message})
}

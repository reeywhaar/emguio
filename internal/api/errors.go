package api

import (
	"encoding/json"
	"net/http"
)

// Refusal codes. The code is the contract; the message is not, and nothing may match on it.
const (
	CodeInvalid              = "invalid"
	CodeUnauthenticated      = "unauthenticated"
	CodeNotFound             = "not_found"
	CodeConflict             = "conflict"
	CodeForbidden            = "forbidden"
	CodeUnsupportedMediaType = "unsupported_media_type"
	CodeBodyTooLarge         = "body_too_large"
	CodeRateLimited          = "rate_limited"
	CodeBusy                 = "busy"
	CodeInternal             = "internal"
)

// errorBody is the shape of every refusal.
//
// Ok is for a caller reading the body without the status line, which is most tool-calling
// harnesses.
type errorBody struct {
	Ok      bool   `json:"ok"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// refuse is the only place a refusal is written, so there is one spelling of the envelope.
// The message names the limit or the value; a refusal a caller cannot act on is a bug.
func refuse(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Code: code, Message: message})
}

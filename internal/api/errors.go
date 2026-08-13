package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/angeltonio/aliasdeck/internal/store"
)

// errorBody is the one JSON shape every error response this package writes
// uses (server-api spec, "Consistent Error Shape"; design.md's Interfaces
// section: {"error":{"code","message","details"}}). Details is omitted
// entirely when there is none — no field on this type, and no call site in
// this package, may carry an internal error's own text, a file path, or a
// SQL detail.
type errorBody struct {
	Error errorFields `json:"error"`
}

type errorFields struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Error codes. This is the closed set every handler in this package draws
// from; a new failure mode adds a constant here, never an inline literal.
const (
	codeUnauthorized     = "unauthorized"
	codeNotFound         = "not_found"
	codeConflict         = "conflict"
	codeInvalidReference = "invalid_reference"
	codeBodyTooLarge     = "body_too_large"
	codeTimeout          = "timeout"
	codeInternal         = "internal"
)

// writeError writes errorBody to w with status. It is the only function in
// this package that writes to w for an error case, so every endpoint's
// failure is byte-for-byte the same shape, differing only in code, message,
// status and (optionally) details.
func writeError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: errorFields{Code: code, Message: message, Details: details}})
}

// writeStoreError maps internal/store's three sentinels to their HTTP
// status, per design decision 18:
//
//   - store.ErrNotFound        -> 404 Not Found
//   - store.ErrConflict        -> 409 Conflict
//   - store.ErrInvalidReference -> 422 Unprocessable Entity — deliberately
//     NOT 409. A dangling foreign-key reference is not a name collision;
//     collapsing it back onto 409 recreates exactly the bug decision 18
//     introduced ErrInvalidReference to fix (a Phase 2 finding: foreign-key
//     violations were being reported as name collisions).
//
// Any other error — a raw driver error, a wrapped fmt.Errorf, anything not
// one of the three sentinels — maps to a generic 500 with a fixed message.
// Its own Error() text is never placed in the response body.
func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "the requested resource was not found", nil)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, codeConflict, "the request conflicts with an existing resource", nil)
	case errors.Is(err, store.ErrInvalidReference):
		writeError(w, http.StatusUnprocessableEntity, codeInvalidReference, "the request references a resource that does not exist", nil)
	default:
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error", nil)
	}
}

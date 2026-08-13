package api

import (
	"encoding/json"
	"net/http"
)

// writeJSON encodes v as w's body at status, with the Content-Type every
// non-error response in this package uses. It is the CRUD/auth handlers'
// counterpart to errors.go's writeError: together they are the only two
// functions that write to a response body in this package.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON decodes r.Body into v, writing a 400 in the shared error shape
// and reporting false when the body is not valid JSON for v's shape.
// Callers MUST return immediately when this reports false — v is not
// necessarily fully populated.
//
// r.Body is already wrapped by withMaxBytes (router.go) by the time any
// handler sees it, so a request body large enough to make this decode fail
// on a *http.MaxBytesError is already the "Bounded Request Size" scenario,
// not a new failure mode this function needs to special-case: it still
// reports false and writes codeInvalidBody, which is accurate — the body
// that arrived, truncated or not, did not decode.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidBody, "the request body is not valid JSON", nil)
		return false
	}
	return true
}

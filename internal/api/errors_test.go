package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/store"
)

// decodedError mirrors errorBody's wire shape for test assertions, decoded
// independently of the production type so a test can fail if the produced
// JSON drifts from {"error":{"code","message","details"}} even if
// errorBody itself were renamed or restructured.
type decodedError struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

// TestWriteStoreErrorUsesOneShapeAcrossDistinctEndpoints is the server-api
// spec's "Consistent Error Shape" requirement: two different simulated
// endpoints, each failing with a different store sentinel, must produce
// wire-identical error envelopes — the same top-level "error" object with
// exactly "code", "message" and (optionally) "details", never anything
// endpoint-specific bolted on beside it.
func TestWriteStoreErrorUsesOneShapeAcrossDistinctEndpoints(t *testing.T) {
	aliasEndpoint := func(w http.ResponseWriter, _ *http.Request) {
		writeStoreError(w, store.ErrNotFound)
	}
	profileEndpoint := func(w http.ResponseWriter, _ *http.Request) {
		writeStoreError(w, store.ErrConflict)
	}

	for _, h := range []http.HandlerFunc{aliasEndpoint, profileEndpoint} {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Errorf("Content-Type = %q, want application/json; charset=utf-8", ct)
		}

		var got decodedError
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding error body %q: %v", rec.Body.String(), err)
		}
		if got.Error.Code == "" {
			t.Errorf("body %q: error.code is empty, want a machine-readable code", rec.Body.String())
		}
		if got.Error.Message == "" {
			t.Errorf("body %q: error.message is empty, want a human-readable message", rec.Body.String())
		}
	}
}

// TestWriteStoreErrorMapsEachSentinelToItsOwnStatus is the exact mapping
// design decision 18 exists to make deliberate: ErrNotFound -> 404,
// ErrConflict -> 409, and — the distinction the sentinel itself was added
// for — ErrInvalidReference -> 422, never 409. Collapsing ErrInvalidReference
// back onto 409 would make a dangling foreign-key reference indistinguishable
// from a name collision again, exactly the bug decision 18 fixed in
// internal/store.
func TestWriteStoreErrorMapsEachSentinelToItsOwnStatus(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"not found", store.ErrNotFound, http.StatusNotFound, codeNotFound},
		{"conflict", store.ErrConflict, http.StatusConflict, codeConflict},
		{"invalid reference", store.ErrInvalidReference, http.StatusUnprocessableEntity, codeInvalidReference},
		{"wrapped not found", errWrap{store.ErrNotFound}, http.StatusNotFound, codeNotFound},
		{"unknown error", errors.New("boom: driver-specific detail nobody should see"), http.StatusInternalServerError, codeInternal},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeStoreError(rec, c.err)

			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
			}

			var got decodedError
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decoding error body %q: %v", rec.Body.String(), err)
			}
			if got.Error.Code != c.wantCode {
				t.Fatalf("error.code = %q, want %q", got.Error.Code, c.wantCode)
			}
		})
	}
}

// TestWriteStoreErrorNeverLeaksInternalErrorText proves the closed-code
// promise directly: an arbitrary internal error's own text (which could
// carry a file path, a driver error, or raw SQL) must never appear
// anywhere in the response body — only the fixed, generic codeInternal
// message does.
func TestWriteStoreErrorNeverLeaksInternalErrorText(t *testing.T) {
	secret := "/var/lib/aliasdeck/server.db: disk I/O error at offset 4096"
	rec := httptest.NewRecorder()
	writeStoreError(rec, errors.New(secret))

	if got := rec.Body.String(); strings.Contains(got, secret) {
		t.Fatalf("response body %q leaked internal error text %q", got, secret)
	}
}

// errWrap lets the mapping test prove errors.Is-based matching survives
// wrapping, matching how a real handler receives errors from internal/store
// (e.g. fmt.Errorf("aliases: create: %w", err)).
type errWrap struct{ err error }

func (e errWrap) Error() string { return "wrapped: " + e.err.Error() }
func (e errWrap) Unwrap() error { return e.err }

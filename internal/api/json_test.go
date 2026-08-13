package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDecodeJSONRejectsUnknownFields is WARNING 6's own RED test:
// decodeJSON must reject a body carrying a field its target struct does
// not declare, rather than silently discarding it — a client that typos a
// field name (e.g. "commnad" instead of "command") must be told, not left
// believing a write it never actually made took effect. Mutation this test
// detects: removing dec.DisallowUnknownFields() from decodeJSON — this
// body would then decode successfully (ok == true) with the typo'd field
// simply dropped.
func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	type target struct {
		Command string `json:"command"`
	}

	req := httptest.NewRequest(http.MethodPost, "/whatever", strings.NewReader(`{"command":"echo hi","commnad":"typo"}`))
	rec := httptest.NewRecorder()

	var v target
	ok := decodeJSON(rec, req, &v)

	if ok {
		t.Fatalf("decodeJSON with an unknown field = true, want false; decoded into %+v", v)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestDecodeJSONAcceptsAKnownFieldBody is the GREEN-path counterpart:
// DisallowUnknownFields must not reject a body whose fields are exactly
// the target's own — the change is a rejection of the unexpected, not an
// accidental tightening of what a legitimate request may send.
func TestDecodeJSONAcceptsAKnownFieldBody(t *testing.T) {
	type target struct {
		Command string `json:"command"`
	}

	req := httptest.NewRequest(http.MethodPost, "/whatever", strings.NewReader(`{"command":"echo hi"}`))
	rec := httptest.NewRecorder()

	var v target
	ok := decodeJSON(rec, req, &v)

	if !ok {
		t.Fatalf("decodeJSON with only known fields = false, want true; body=%s", rec.Body.String())
	}
	if v.Command != "echo hi" {
		t.Fatalf("v.Command = %q, want %q", v.Command, "echo hi")
	}
}

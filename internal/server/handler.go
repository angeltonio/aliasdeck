package server

import (
	"io"
	"net/http"
)

// healthPath is the one route this phase wires directly, ahead of
// internal/api's full router (Phase 5). Server-runtime spec, "Health
// Endpoint Requires No Authentication": reachable without a session or
// device token.
const healthPath = "GET /api/v1/health"

// newHandler builds the HTTP handler Run serves. Phase 5 replaces this
// mux's contents with internal/api's route slice, but never removes or
// re-guards healthPath: the health endpoint must stay reachable without
// authentication for as long as this server exists.
func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(healthPath, handleHealth)
	return mux
}

// handleHealth reports readiness with a fixed, minimal body. It
// deliberately exposes nothing an unauthenticated caller should not have:
// no schema version, no build metadata, no filesystem path, no database
// state — just confirmation that the process is up and its handler chain
// is being reached.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `{"status":"ok"}`+"\n")
}

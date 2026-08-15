package api

import (
	"net/http"

	"github.com/angeltonio/aliasdeck/internal/auth"
)

const heartbeatPattern = "/api/v1/heartbeat"

// handleHeartbeat records that the authenticated device can currently reach
// the control plane. Unlike sync, it deliberately does not change alias-sync
// state, platform, or shell.
func (a *api) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	subj, ok := auth.SubjectFromContext(r.Context())
	if !ok {
		writeUnauthorizedDevice(w)
		return
	}
	if err := a.store.Devices().Heartbeat(r.Context(), subj.SubjectID, a.now()); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

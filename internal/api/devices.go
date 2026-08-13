package api

import (
	"net/http"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// devicesPattern and devicePattern are the device collection/read/update/
// delete routes; deviceRevokePattern and deviceTokenPattern are the two
// device-scoped auth actions (server-auth spec's "Immediate Device
// Revocation" and "Device Token Rotation").
//
// There is deliberately no POST devicesPattern: DeviceRepo has no Create —
// a device is born only through devicesRegisterPattern's enrollment-token
// exchange (auth.go), which is what makes enrollment atomic.
const (
	devicesPattern      = "/api/v1/devices"
	devicePattern       = "/api/v1/devices/{id}"
	deviceRevokePattern = "/api/v1/devices/{id}/revoke"
	deviceTokenPattern  = "/api/v1/devices/{id}/token"
)

// deviceTokenResponse is what both device registration (auth.go) and
// rotation return: the device's id and a freshly minted device token wire
// value, delivered exactly once. Neither handler returns this token again
// on any later read — DeviceRepo has no method that would let one, and
// Token.SecretHash never leaves this package unhashed.
type deviceTokenResponse struct {
	DeviceID    string `json:"deviceId"`
	DeviceToken string `json:"deviceToken"`
}

func (a *api) handleDevicesList(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.Devices().List(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *api) handleDevicesGet(w http.ResponseWriter, r *http.Request) {
	out, err := a.store.Devices().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDevicesUpdate replaces a device's name and profile membership
// (DeviceRepo.Update's documented scope) — platform, shell and the
// last-seen/last-sync timestamps are sync's own write (Phase 6, "Touch"),
// never an operator edit.
func (a *api) handleDevicesUpdate(w http.ResponseWriter, r *http.Request) {
	var in domain.Device
	if !decodeJSON(w, r, &in) {
		return
	}
	in.ID = r.PathValue("id")
	out, err := a.store.Devices().Update(r.Context(), in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) handleDevicesDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Devices().Delete(r.Context(), r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDevicesRevoke is the server-auth spec's "Immediate Device
// Revocation" scenario: it marks the device row revoked AND revokes every
// device-kind token belonging to it in the same request, so the device's
// very next sync attempt — whichever token it happens to present — fails
// authentication. Revoking only the device row (and leaving an
// already-issued token verifiable) would satisfy neither "immediately" nor
// "on its very next use".
func (a *api) handleDevicesRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	now := a.now()

	if err := a.store.Devices().Revoke(r.Context(), id, now); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := a.store.Tokens().RevokeSubject(r.Context(), store.TokenKindDevice, id, now); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDevicesRotateToken is the server-auth spec's "Device Token
// Rotation" scenario: it revokes every existing device-kind token for id
// before minting and persisting the replacement, so the previous token is
// invalid the instant this call returns — there is no window where both
// the old and new token authenticate.
func (a *api) handleDevicesRotateToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if _, err := a.store.Devices().Get(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}

	now := a.now()
	if err := a.store.Tokens().RevokeSubject(r.Context(), store.TokenKindDevice, id, now); err != nil {
		writeStoreError(w, err)
		return
	}

	minted, err := auth.Mint(store.TokenKindDevice)
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error", nil)
		return
	}
	if err := a.store.Tokens().Create(r.Context(), store.Token{
		Kind:       store.TokenKindDevice,
		SubjectID:  id,
		Lookup:     minted.Lookup,
		SecretHash: minted.SecretHash,
		CreatedAt:  now,
	}); err != nil {
		// Deliberately accepted, not reordered (bounded-review finding,
		// WARNING 4; design decision 27). If this write fails after
		// RevokeSubject above already succeeded, the device is left with
		// zero valid device-kind tokens until this endpoint is called
		// again. That retry is safe by construction: RevokeSubject only
		// touches rows with revoked_at IS NULL, so a retry finds nothing
		// left to revoke (a no-op) and proceeds straight to minting the
		// replacement — no manual cleanup required. Reordering to
		// mint-then-revoke was considered and rejected: RevokeSubject's own
		// filter (kind + subjectID + unrevoked) cannot distinguish an old
		// token from the brand-new one for the same device without a
		// store-side change, which is out of this correction's scope
		// (internal/store unchanged) — mint-then-revoke as written would
		// revoke the token it just minted.
		writeError(w, http.StatusInternalServerError, codeInternal,
			"the previous device token was revoked but a replacement could not be issued; retry this request", map[string]any{"deviceId": id})
		return
	}

	writeJSON(w, http.StatusOK, deviceTokenResponse{DeviceID: id, DeviceToken: minted.Wire})
}

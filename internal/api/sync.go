package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/sync"
)

// syncPattern is the only device-gated route this server exposes (server-
// sync spec's "Server-Side Resolution Reuses domain.Resolve").
const syncPattern = "/api/v1/sync"

// syncResponse is the wire shape design decision 9 fixes exactly:
// {revision, device{id,name,platform,shell,profileIds},
// aliases[{name,command,description}], generatedAt}. There is deliberately
// no field anywhere in this type or its children that could carry a
// server-side alias id, a rendered string, or shell syntax (threat matrix:
// "Sync response as hostile input" is the client's problem to guard
// against; this type is what keeps the server from handing it anything
// worse than neutral data in the first place).
type syncResponse struct {
	Revision    string      `json:"revision"`
	Device      syncDevice  `json:"device"`
	Aliases     []syncAlias `json:"aliases"`
	GeneratedAt time.Time   `json:"generatedAt"`
}

// syncDevice carries only the fields decision 9 names — notably not
// Hostname, Architecture, ClientVersion, or either timestamp domain.Device
// itself has. Those are operator-facing fields (visible through
// GET /api/v1/devices/{id}), not sync response fields.
type syncDevice struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Platform   domain.Platform `json:"platform"`
	Shell      domain.Shell    `json:"shell"`
	ProfileIDs []string        `json:"profileIds"`
}

// syncAlias is deliberately the narrowest possible view of domain.Alias:
// name, command, description. No ID field exists on this type — not "ID
// omitempty", not present at all — which is what makes "add a server-side
// alias ID to the sync response" a compile-time change to this struct
// literal, not a value that could silently start being populated.
type syncAlias struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
}

// validPlatforms and validShells are the closed sets handleSync names in a
// 400 (design decision 10: "an unknown platform or shell is a 400 naming
// the valid set, never a silent default"). Computed once from
// domain.AllPlatforms/AllShells rather than hand-duplicated, so a future
// platform or shell addition to internal/domain does not silently leave
// this message out of date.
var (
	validPlatforms = joinDomainValues(domain.AllPlatforms)
	validShells    = joinDomainValuesShells(domain.AllShells)
)

func joinDomainValues(ps []domain.Platform) string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.String()
	}
	return strings.Join(names, ", ")
}

func joinDomainValuesShells(ss []domain.Shell) string {
	names := make([]string, len(ss))
	for i, s := range ss {
		names[i] = s.String()
	}
	return strings.Join(names, ", ")
}

// handleSync is the server-sync spec's only endpoint: it resolves the
// authenticated device's aliases through sync.Resolve (which calls
// domain.Resolve, never a second filtering implementation — design decision
// 4), persists the platform/shell this request itself reports, and stamps
// last_seen_at/last_sync_at on the same GET (design decision 10). Query
// parameters, not a body: this route only ever receives a GET (design
// decision 9's rejected alternative was a POST with a body).
//
// Profile membership is never taken from the request — decision 9/the
// server-sync spec's "Client Owns Platform/Shell; Server Owns Profile
// Membership" requirement — it always comes from the stored device row
// fetched below.
func (a *api) handleSync(w http.ResponseWriter, r *http.Request) {
	subj, ok := auth.SubjectFromContext(r.Context())
	if !ok {
		writeUnauthorizedDevice(w)
		return
	}

	platform, perr := parseSyncPlatform(r.URL.Query().Get("platform"))
	if perr != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, perr.Error(), nil)
		return
	}
	shell, serr := parseSyncShell(r.URL.Query().Get("shell"))
	if serr != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, serr.Error(), nil)
		return
	}

	dev, err := a.store.Devices().Get(r.Context(), subj.SubjectID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// The client's report for THIS request overrides whatever platform/shell
	// was stored from a previous sync — resolution always uses what the
	// device says it is right now. ProfileIDs is left exactly as read from
	// the store: the server, never the client, owns profile membership.
	dev.Platform = platform
	dev.Shell = shell

	resolved, err := sync.Resolve(r.Context(), a.store, dev)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	now := a.now()
	if err := a.store.Devices().Touch(r.Context(), subj.SubjectID, platform, shell, now); err != nil {
		writeStoreError(w, err)
		return
	}

	aliases := make([]syncAlias, 0, len(resolved.Aliases))
	for _, al := range resolved.Aliases {
		aliases = append(aliases, syncAlias{Name: al.Name, Command: al.Command, Description: al.Description})
	}

	writeJSON(w, http.StatusOK, syncResponse{
		Revision: resolved.Revision,
		Device: syncDevice{
			ID:         resolved.Device.ID,
			Name:       resolved.Device.Name,
			Platform:   resolved.Device.Platform,
			Shell:      resolved.Device.Shell,
			ProfileIDs: resolved.Device.ProfileIDs,
		},
		Aliases:     aliases,
		GeneratedAt: now,
	})
}

// parseSyncPlatform and parseSyncShell reject an empty or unrecognized
// query value identically — an omitted parameter is exactly as unusable as
// a typo'd one, and both must name the valid set rather than silently
// defaulting to something (design decision 10).
func parseSyncPlatform(raw string) (domain.Platform, error) {
	if raw == "" {
		return "", fmt.Errorf("the platform query parameter is required, must be one of: %s", validPlatforms)
	}
	p := domain.Platform(raw)
	if !p.Valid() {
		return "", fmt.Errorf("unknown platform %q, must be one of: %s", raw, validPlatforms)
	}
	return p, nil
}

func parseSyncShell(raw string) (domain.Shell, error) {
	if raw == "" {
		return "", fmt.Errorf("the shell query parameter is required, must be one of: %s", validShells)
	}
	sh := domain.Shell(raw)
	if !sh.Valid() {
		return "", fmt.Errorf("unknown shell %q, must be one of: %s", raw, validShells)
	}
	return sh, nil
}

// writeUnauthorizedDevice is syncPattern's own auth.Refuse (router.go),
// distinct from every other guarded route's generic writeUnauthorized.
// Every other route in this API is reachable again by the operator logging
// back in — but a device has no session to fall back on, and "unauthorized"
// gives it nothing to act on. This message is uniform across every failure
// RequireKind can hit for this route (missing header, malformed token,
// unknown lookup, wrong secret, wrong kind, expired, or revoked) — it does
// not, and must not, distinguish which one occurred: doing so would turn
// this endpoint into an oracle for enumerating which device-token lookups
// once existed (threat matrix: token handling), exactly the property
// RequireKind's own uniform 401 (design decision 25) already protects for
// every other route. What changes here is only that the single message
// covering all of those cases names the one action that recovers from every
// one of them: register this device again.
func writeUnauthorizedDevice(w http.ResponseWriter) {
	writeError(w, http.StatusUnauthorized, codeInvalidToken,
		"this device's token is missing, invalid, expired, or revoked; register this device again to obtain a new one", nil)
}

package web

import (
	"net/http"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// enrollmentTokenTTL mirrors internal/api's own defaultEnrollmentTTL
// (design's token lifetime table: "15 min default"). This prototype's
// mint-a-token button always uses the default; it does not expose the
// TTL/profile-scoping options POST /api/v1/enrollment-tokens accepts —
// mint-and-show only, per the prototype brief.
const enrollmentTokenTTL = 15 * time.Minute

// deviceRow is devices.html's per-row view of a domain.Device, with
// LastSyncAt pre-formatted so the template does no time arithmetic of its
// own.
type deviceRow struct {
	Name       string
	Platform   string
	Shell      string
	LastSyncAt string
	Synced     bool
}

type devicesPageData struct {
	Title   string
	Active  string
	Devices []deviceRow
}

func (a *webapp) handleDevicesPage(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.Devices().List(r.Context())
	if err != nil {
		http.Error(w, "could not load devices", http.StatusInternalServerError)
		return
	}

	rows := make([]deviceRow, 0, len(list))
	for _, d := range list {
		row := deviceRow{Name: d.Name, Platform: string(d.Platform), Shell: string(d.Shell), LastSyncAt: "never"}
		if d.LastSyncAt != nil {
			row.LastSyncAt = d.LastSyncAt.Format("2006-01-02 15:04 MST")
			row.Synced = true
		}
		rows = append(rows, row)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.devices.ExecuteTemplate(w, "base", devicesPageData{Title: "Devices", Active: "devices", Devices: rows})
}

func (a *webapp) handleDevicesAddPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.devicesAdd.ExecuteTemplate(w, "base", devicesPageData{Title: "Add device", Active: "devices"})
}

// mintResultData is device_mint_result.html's data shape: the exact
// copy-pasteable commands the prototype brief asks for.
type mintResultData struct {
	Token     string
	URL       string
	ExpiresAt string
}

// handleDevicesMintToken is the "copy the commands" flow's entire
// backend: mint a single-use enrollment token against the same
// store.TokenRepo internal/api/auth.go's handleEnrollmentTokensCreate
// writes to, and render the exact two lines the new machine needs.
//
// The URL is derived from the request itself (scheme + r.Host) rather
// than from configuration — a pragmatic prototype shortcut. A real
// implementation of this flow should let the operator confirm or override
// the externally-reachable address, since r.Host is whatever the browser
// happened to connect to (loopback, a LAN IP, or a reverse-proxy's
// hostname) and is not guaranteed to be reachable from the machine
// running `aliasdeck register`.
func (a *webapp) handleDevicesMintToken(w http.ResponseWriter, r *http.Request) {
	minted, err := auth.Mint(store.TokenKindEnrollment)
	if err != nil {
		http.Error(w, "could not mint an enrollment token", http.StatusInternalServerError)
		return
	}

	now := a.now()
	expiresAt := now.Add(enrollmentTokenTTL)
	if err := a.store.Tokens().Create(r.Context(), store.Token{
		Kind:       store.TokenKindEnrollment,
		Lookup:     minted.Lookup,
		SecretHash: minted.SecretHash,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
	}); err != nil {
		http.Error(w, "could not mint an enrollment token", http.StatusInternalServerError)
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.mintResult.ExecuteTemplate(w, "device_mint_result", mintResultData{
		Token:     minted.Wire,
		URL:       scheme + "://" + r.Host,
		ExpiresAt: expiresAt.Format("2006-01-02 15:04:05 MST"),
	})
}

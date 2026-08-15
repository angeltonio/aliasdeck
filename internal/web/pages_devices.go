package web

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/google/uuid"
)

// enrollmentTokenTTL mirrors internal/api's own defaultEnrollmentTTL
// (design's token lifetime table: "15 min default"). This prototype's
// mint-a-token button always uses the default; it does not expose the
// TTL/profile-scoping options POST /api/v1/enrollment-tokens accepts —
// mint-and-show only, per the prototype brief.
const enrollmentTokenTTL = 15 * time.Minute

// Prototype device freshness is intentionally observational: it describes
// timestamps written by successful syncs and never schedules or initiates one.
// A device is Recent when both fields are at most 15 minutes old, Delayed when
// either is older than that but no older than 24 hours, and Stale when either
// is older than 24 hours. A missing timestamp is reported separately so an
// enrolled device that has not synced is not mistaken for an inactive one.
const (
	prototypeDeviceFreshWithin = 15 * time.Minute
	prototypeDeviceStaleAfter  = 24 * time.Hour
)

// deviceRow is devices.html's per-row view of a domain.Device, with all time
// arithmetic resolved through webapp.now before the template renders it.
type deviceRow struct {
	Name         string
	Platform     string
	Shell        string
	LastSeenAt   *deviceTimestamp
	LastSyncAt   *deviceTimestamp
	Synced       bool
	Status       string
	StatusClass  string
	StatusDetail string
}

type devicesPageData struct {
	Title   string
	Active  string
	Devices []deviceRow
}

// deviceTimestamp preserves the server-authoritative UTC instant for semantic
// HTML while providing a readable UTC fallback when JavaScript is unavailable.
type deviceTimestamp struct {
	DateTime string
	UTC      string
}

func newDeviceTimestamp(at *time.Time) *deviceTimestamp {
	if at == nil {
		return nil
	}

	utc := at.UTC()
	return &deviceTimestamp{
		DateTime: utc.Format(time.RFC3339Nano),
		UTC:      utc.Format("2006-01-02 15:04 UTC"),
	}
}

func (a *webapp) handleDevicesPage(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.Devices().List(r.Context())
	if err != nil {
		http.Error(w, "could not load devices", http.StatusInternalServerError)
		return
	}

	now := a.now()
	rows := make([]deviceRow, 0, len(list))
	for _, d := range list {
		row := deviceRow{Name: d.Name, Platform: string(d.Platform), Shell: string(d.Shell)}
		row.LastSeenAt = newDeviceTimestamp(d.LastSeenAt)
		row.LastSyncAt = newDeviceTimestamp(d.LastSyncAt)
		row.Synced = row.LastSyncAt != nil
		status := classifyDeviceFreshness(d.LastSeenAt, d.LastSyncAt, now)
		row.Status = status.label
		row.StatusClass = status.class
		row.StatusDetail = status.detail
		rows = append(rows, row)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.devices.ExecuteTemplate(w, "base", devicesPageData{Title: "Devices", Active: "devices", Devices: rows})
}

type deviceFreshness struct {
	label  string
	class  string
	detail string
}

func classifyDeviceFreshness(lastSeenAt, lastSyncAt *time.Time, now time.Time) deviceFreshness {
	if lastSyncAt == nil {
		return deviceFreshness{label: "Not synced", class: "stale", detail: "This device has not completed a sync yet."}
	}
	if lastSeenAt == nil {
		return deviceFreshness{label: "Not seen", class: "stale", detail: "This device has not checked in yet."}
	}

	if now.Sub(*lastSeenAt) > prototypeDeviceStaleAfter {
		return deviceFreshness{label: "Stale", class: "stale", detail: "This device has not checked in for over 24 hours."}
	}
	if now.Sub(*lastSyncAt) > prototypeDeviceStaleAfter {
		return deviceFreshness{label: "Sync overdue", class: "stale", detail: "This device has not synced for over 24 hours."}
	}
	if now.Sub(*lastSeenAt) > prototypeDeviceFreshWithin || now.Sub(*lastSyncAt) > prototypeDeviceFreshWithin {
		return deviceFreshness{label: "Delayed", class: "stale", detail: "This device was last seen or synced over 15 minutes ago."}
	}

	return deviceFreshness{label: "Recent", class: "ok", detail: "This device checked in and synced within the last 15 minutes."}
}

func (a *webapp) handleDevicesAddPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.devicesAdd.ExecuteTemplate(w, "base", devicesPageData{Title: "Add device", Active: "devices"})
}

// mintResultData is device_mint_result.html's data shape: the exact
// copy-pasteable commands the prototype brief asks for.
type mintResultData struct {
	Command   string
	ExpiresAt string
	StatusID  string
	Message   string
}

// enrollmentTracker keeps the browser-only correlation handle separate from
// the enrollment wire token. The handle is random, has no secret material,
// and is bound to the exact operator session that minted it. This is
// deliberately process-local: restarting the prototype drops pending UI
// polls but never drops or weakens the actual enrollment token in the store.
type enrollmentTracker struct {
	mu     sync.Mutex
	states map[string]enrollmentState
}

type enrollmentState struct {
	sessionTokenID string
	lookup         string
	expiresAt      time.Time
}

func newEnrollmentTracker() *enrollmentTracker {
	return &enrollmentTracker{states: make(map[string]enrollmentState)}
}

func (t *enrollmentTracker) create(sessionTokenID, lookup string, expiresAt time.Time) string {
	id := uuid.NewString()
	t.mu.Lock()
	t.states[id] = enrollmentState{sessionTokenID: sessionTokenID, lookup: lookup, expiresAt: expiresAt}
	t.mu.Unlock()
	return id
}

// get returns a state only to the session that created it. Callers must keep
// the resulting lookup server-side; it is an identifier, not a browser
// capability, and must not be put in the polling URL.
func (t *enrollmentTracker) get(id, sessionTokenID string) (enrollmentState, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, ok := t.states[id]
	if !ok || state.sessionTokenID != sessionTokenID {
		return enrollmentState{}, false
	}
	return state, true
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func mintCommand(url, token string) string {
	// `init` cannot install a function into the parent shell that invoked it.
	// Explicitly source the file after sync so the pasted POSIX flow has the
	// same observable result even before the user starts a new shell. The path
	// expression follows AliasDeck's supported ALIASDECK_HOME/XDG defaults and
	// derives the extension from the current zsh/bash process.
	aliasesPath := `"${ALIASDECK_HOME:-${XDG_CONFIG_HOME:-$HOME/.config}/aliasdeck}/aliases.${SHELL##*/}"`
	return "aliasdeck init --yes --skip-initial-sync && aliasdeck register --url " + shellQuote(url) + " --token " + shellQuote(token) + " && aliasdeck sync && . " + aliasesPath
}

// handleDevicesMintToken is the "copy the commands" flow's entire
// backend: mint a single-use enrollment token against the same
// store.TokenRepo internal/api/auth.go's handleEnrollmentTokensCreate
// writes to, and render the one-flow command the new machine needs.
//
// The URL is derived from the request itself (scheme + r.Host) rather
// than from configuration — a pragmatic prototype shortcut. A real
// implementation of this flow should let the operator confirm or override
// the externally-reachable address, since r.Host is whatever the browser
// happened to connect to (loopback, a LAN IP, or a reverse-proxy's
// hostname) and is not guaranteed to be reachable from the machine
// running `aliasdeck register`.
func (a *webapp) handleDevicesMintToken(w http.ResponseWriter, r *http.Request) {
	subject, ok := subjectFromContext(r.Context())
	if !ok {
		http.Error(w, "could not mint an enrollment token", http.StatusInternalServerError)
		return
	}

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
	statusID := a.enrollments.create(subject.TokenID, minted.Lookup, expiresAt)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.mintResult.ExecuteTemplate(w, "device_mint_result", mintResultData{
		Command:   mintCommand(scheme+"://"+r.Host, minted.Wire),
		ExpiresAt: expiresAt.Format("2006-01-02 15:04:05 MST"),
		StatusID:  statusID,
		Message:   "Waiting for the new machine to enroll and complete its first sync…",
	})
}

type enrollmentStatusData struct {
	StatusID string
	Message  string
}

// handleDeviceEnrollmentStatus is the htmx polling endpoint for one mint
// operation. It observes the stored lookup only after proving the same
// browser session that minted the opaque status ID is asking; neither the
// enrollment secret nor its lookup appears in the request URL.
func (a *webapp) handleDeviceEnrollmentStatus(w http.ResponseWriter, r *http.Request) {
	subject, ok := subjectFromContext(r.Context())
	if !ok {
		http.NotFound(w, r)
		return
	}

	state, ok := a.enrollments.get(r.PathValue("id"), subject.TokenID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if !a.now().Before(state.expiresAt) {
		a.renderEnrollmentStatus(w, "device_enrollment_expired", enrollmentStatusData{Message: "This enrollment token expired. Mint a new token to try again."})
		return
	}

	token, err := a.store.Tokens().ByLookup(r.Context(), state.lookup)
	if err != nil {
		http.Error(w, "could not check enrollment status", http.StatusInternalServerError)
		return
	}

	if token.UsedAt.IsZero() || token.SubjectID == "" {
		a.renderEnrollmentStatus(w, "device_enrollment_pending", enrollmentStatusData{
			StatusID: r.PathValue("id"),
			Message:  "Waiting for the new machine to enroll and complete its first sync…",
		})
		return
	}

	device, err := a.store.Devices().Get(r.Context(), token.SubjectID)
	if err != nil {
		http.Error(w, "could not check enrollment status", http.StatusInternalServerError)
		return
	}
	if device.LastSyncAt == nil {
		a.renderEnrollmentStatus(w, "device_enrollment_pending", enrollmentStatusData{
			StatusID: r.PathValue("id"),
			Message:  "Device enrolled. Waiting for its first sync…",
		})
		return
	}

	w.Header().Set("HX-Redirect", "/devices")
	a.renderEnrollmentStatus(w, "device_enrollment_complete", enrollmentStatusData{Message: "Device enrolled and synced. Redirecting…"})
}

func (a *webapp) renderEnrollmentStatus(w http.ResponseWriter, name string, data enrollmentStatusData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.mintResult.ExecuteTemplate(w, name, data)
}

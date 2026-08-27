package web

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/angeltonio/aliasdeck/internal/validate"
	"github.com/angeltonio/aliasdeck/internal/watchconfig"
	"github.com/google/uuid"
)

// enrollmentTokenTTL mirrors internal/api's own defaultEnrollmentTTL
// (design's token lifetime table: "15 min default"). The web UI's
// mint-a-token button always uses the default; it does not expose the
// TTL/profile-scoping options POST /api/v1/enrollment-tokens accepts —
// mint-and-show only, as one guarded operation.
const enrollmentTokenTTL = 15 * time.Minute

// Device freshness is intentionally observational: it describes
// timestamps written by successful syncs and never schedules or initiates one.
// A device is Recent when both fields are at most 15 minutes old, Delayed when
// either is older than that but no older than 24 hours, and Stale when either
// is older than 24 hours. A missing timestamp is reported separately so an
// enrolled device that has not synced is not mistaken for an inactive one.
const (
	deviceFreshWithin = 15 * time.Minute
	deviceStaleAfter  = 24 * time.Hour
)

// deviceRow is devices.html's per-row view of a domain.Device, with all time
// arithmetic resolved through webapp.now before the template renders it.
type deviceRow struct {
	ID           string
	Name         string
	Platform     string
	Shell        string
	LastSeenAt   *deviceTimestamp
	LastSyncAt   *deviceTimestamp
	Synced       bool
	Status       string
	StatusClass  string
	StatusDetail string
	// Groups is every group that exists, each flagged with whether this
	// device belongs to it. The read-only row shows only the members; the
	// edit row needs the full set to render an unchecked box for the groups
	// the device could be moved into.
	Groups []deviceGroup
}

// deviceGroup pairs one profile with this device's membership in it.
type deviceGroup struct {
	ID     string
	Name   string
	Member bool
}

type devicesPageData struct {
	pageData
	Title   string
	Active  string
	Devices []deviceRow
	// EditingID names the one device rendered as an inline edit row.
	EditingID   string
	FormError   string
	Frequencies []enrollmentFrequencyPreset
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
	rows, err := a.deviceRows(r)
	if err != nil {
		http.Error(w, translate(requestLanguage(r), "error.device_load"), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	view := pageDataFor(r)
	_ = a.tmpl.devices.ExecuteTemplate(w, "base", devicesPageData{pageData: view, Title: translate(view.Lang, "devices.title"), Active: "devices", Devices: rows})
}

// deviceRows resolves every device into its rendered row, including the full
// group list each row needs to offer a membership checkbox. Groups are read
// once for the whole page rather than per device.
func (a *webapp) deviceRows(r *http.Request) ([]deviceRow, error) {
	list, err := a.store.Devices().List(r.Context())
	if err != nil {
		return nil, err
	}
	groups, err := a.store.Profiles().List(r.Context())
	if err != nil {
		return nil, err
	}

	now := a.now()
	rows := make([]deviceRow, 0, len(list))
	for _, d := range list {
		row := deviceRow{ID: d.ID, Name: d.Name, Platform: string(d.Platform), Shell: string(d.Shell)}
		row.LastSeenAt = newDeviceTimestamp(d.LastSeenAt)
		row.LastSyncAt = newDeviceTimestamp(d.LastSyncAt)
		row.Synced = row.LastSyncAt != nil
		status := classifyDeviceFreshnessForLanguage(d.LastSeenAt, d.LastSyncAt, now, requestLanguage(r))
		row.Status = status.label
		row.StatusClass = status.class
		row.StatusDetail = status.detail

		member := make(map[string]bool, len(d.ProfileIDs))
		for _, id := range d.ProfileIDs {
			member[id] = true
		}
		row.Groups = make([]deviceGroup, 0, len(groups))
		for _, g := range groups {
			row.Groups = append(row.Groups, deviceGroup{ID: g.ID, Name: g.Name, Member: member[g.ID]})
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// handleDevicesEdit and handleDevicesPanel are the open/cancel pair the alias
// and group screens already use.
func (a *webapp) handleDevicesEdit(w http.ResponseWriter, r *http.Request) {
	a.respondDevicePanelEditing(r, w, http.StatusOK, r.PathValue("id"), "")
}

func (a *webapp) handleDevicesPanel(w http.ResponseWriter, r *http.Request) {
	a.respondDevicePanel(r, w, http.StatusOK, "")
}

// handleDevicesUpdate renames a device and sets which groups it belongs to.
//
// Group membership is otherwise fixed at enrollment: tokenRepo.ConsumeEnrollment
// takes the profiles from the enrollment token and nothing afterwards could
// change them from the browser, so a machine enrolled into the wrong group
// stayed there. store.DeviceRepo.Update writes only the name and the
// membership join rows — platform, shell and the sync timestamps are
// observed facts the server records, never operator input — so those are
// read-only in the row and untouched here.
func (a *webapp) handleDevicesUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	lang := requestLanguage(r)

	if err := r.ParseForm(); err != nil {
		a.respondDevicePanelEditing(r, w, http.StatusBadRequest, id, translate(lang, "error.device_form"))
		return
	}

	existing, err := a.store.Devices().Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.respondDevicePanel(r, w, http.StatusNotFound, translate(lang, "error.device_missing"))
			return
		}
		http.Error(w, translate(lang, "error.device_load"), http.StatusInternalServerError)
		return
	}

	updated := existing
	updated.Name = strings.TrimSpace(r.FormValue("name"))
	if updated.Name == "" {
		a.respondDevicePanelEditing(r, w, http.StatusBadRequest, id, translate(lang, "error.device_name_required"))
		return
	}
	if err := validate.Description(updated.Name); err != nil {
		a.respondDevicePanelEditing(r, w, http.StatusBadRequest, id, localizeValidationError(lang, err))
		return
	}

	// An unchecked box sends nothing, so the absent form key is what
	// "belongs to no group" looks like — exactly the state an operator
	// removing the last membership is asking for.
	updated.ProfileIDs = r.Form["groups"]

	if _, err := a.store.Devices().Update(r.Context(), updated); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			a.respondDevicePanel(r, w, http.StatusNotFound, translate(lang, "error.device_missing"))
		case errors.Is(err, store.ErrInvalidReference):
			a.respondDevicePanelEditing(r, w, http.StatusBadRequest, id, translate(lang, "error.device_group_missing"))
		default:
			a.respondDevicePanelEditing(r, w, http.StatusInternalServerError, id, formatted(lang, "error.device_update", err.Error()))
		}
		return
	}
	a.respondDevicePanel(r, w, http.StatusOK, "")
}

func (a *webapp) respondDevicePanel(r *http.Request, w http.ResponseWriter, status int, formError string) {
	a.respondDevicePanelEditing(r, w, status, "", formError)
}

func (a *webapp) respondDevicePanelEditing(r *http.Request, w http.ResponseWriter, status int, editingID, formError string) {
	rows, err := a.deviceRows(r)
	if err != nil {
		http.Error(w, translate(requestLanguage(r), "error.device_load"), http.StatusInternalServerError)
		return
	}
	a.writePanel(w, r, status, a.tmpl.devicePanel, "device_panel", devicesPageData{
		pageData: pageDataFor(r), Devices: rows, EditingID: editingID, FormError: formError,
	})
}

type deviceFreshness struct {
	label  string
	class  string
	detail string
}

func classifyDeviceFreshness(lastSeenAt, lastSyncAt *time.Time, now time.Time) deviceFreshness {
	return classifyDeviceFreshnessForLanguage(lastSeenAt, lastSyncAt, now, languageEnglish)
}

func classifyDeviceFreshnessForLanguage(lastSeenAt, lastSyncAt *time.Time, now time.Time, lang language) deviceFreshness {
	if lastSyncAt == nil {
		return deviceFreshness{label: translate(lang, "devices.status.not_synced"), class: "stale", detail: translate(lang, "devices.detail.not_synced")}
	}
	if lastSeenAt == nil {
		return deviceFreshness{label: translate(lang, "devices.status.not_seen"), class: "stale", detail: translate(lang, "devices.detail.not_seen")}
	}

	if now.Sub(*lastSeenAt) > deviceStaleAfter {
		return deviceFreshness{label: translate(lang, "devices.status.stale"), class: "stale", detail: translate(lang, "devices.detail.stale")}
	}
	if now.Sub(*lastSyncAt) > deviceStaleAfter {
		return deviceFreshness{label: translate(lang, "devices.status.sync_overdue"), class: "stale", detail: translate(lang, "devices.detail.sync_overdue")}
	}
	if now.Sub(*lastSeenAt) > deviceFreshWithin || now.Sub(*lastSyncAt) > deviceFreshWithin {
		return deviceFreshness{label: translate(lang, "devices.status.delayed"), class: "stale", detail: translate(lang, "devices.detail.delayed")}
	}

	return deviceFreshness{label: translate(lang, "devices.status.recent"), class: "ok", detail: translate(lang, "devices.detail.recent")}
}

func (a *webapp) handleDevicesAddPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	view := pageDataFor(r)
	_ = a.tmpl.devicesAdd.ExecuteTemplate(w, "base", devicesPageData{pageData: view, Title: translate(view.Lang, "add.title"), Active: "devices", Frequencies: enrollmentFrequencyPresetsForLanguage(a.watchInterval(), view.Lang)})
}

// mintResultData is device_mint_result.html's data shape: the exact
// copy-pasteable setup command shown to the operator.
type mintResultData struct {
	pageData
	Command           string
	ManualCommand     string
	FrequencyCommands []enrollmentFrequencyCommand
	ExpiresAt         string
	StatusID          string
	Message           string
}

type enrollmentFrequencyPreset struct {
	Value    string
	Label    string
	Interval time.Duration
	Selected bool
}

type enrollmentFrequencyCommand struct {
	Value   string
	Command string
}

func enrollmentFrequencyPresets(selected time.Duration) []enrollmentFrequencyPreset {
	return enrollmentFrequencyPresetsForLanguage(selected, languageEnglish)
}

func enrollmentFrequencyPresetsForLanguage(selected time.Duration, lang language) []enrollmentFrequencyPreset {
	presets := []enrollmentFrequencyPreset{
		{Value: "5s", Label: translate(lang, "frequency.5s"), Interval: 5 * time.Second},
		{Value: "30s", Label: translate(lang, "frequency.30s"), Interval: 30 * time.Second},
		{Value: "1m", Label: translate(lang, "frequency.1m"), Interval: time.Minute},
		{Value: "5m", Label: translate(lang, "frequency.5m"), Interval: 5 * time.Minute},
	}
	for i := range presets {
		presets[i].Selected = presets[i].Interval == selected
	}
	return presets
}

func resolveEnrollmentFrequency(raw string, fallback time.Duration) time.Duration {
	for _, preset := range enrollmentFrequencyPresets(fallback) {
		if raw == preset.Value {
			return preset.Interval
		}
	}
	if watchconfig.ValidateEnrollment(fallback) == nil {
		return fallback
	}
	return watchconfig.DefaultInterval
}

// enrollmentTracker keeps the browser-only correlation handle separate from
// the enrollment wire token. The handle is random, has no secret material,
// and is bound to the exact operator session that minted it. This is
// deliberately process-local: restarting the server drops pending UI
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

func mintCommand(url, token string, autoSync bool, interval time.Duration) string {
	// `init` cannot install a function into the parent shell that invoked it.
	// Explicitly source the file after sync so the pasted POSIX flow has the
	// same observable result even before the user starts a new shell. The path
	// expression follows AliasDeck's supported ALIASDECK_HOME/XDG defaults and
	// derives the extension from the current zsh/bash process.
	aliasesPath := `"${ALIASDECK_HOME:-${XDG_CONFIG_HOME:-$HOME/.config}/aliasdeck}/aliases.${SHELL##*/}"`
	command := "aliasdeck init --yes --skip-initial-sync && aliasdeck register --url " + shellQuote(url) + " --token " + shellQuote(token) + " && aliasdeck sync"
	if autoSync {
		// Persistent background startup is intentionally macOS-only. Keep the
		// rest of onboarding useful on unsupported platforms without claiming
		// that a background service was installed there.
		command += ` && if [ "$(uname -s)" = Darwin ]; then aliasdeck agent install --interval ` + shellQuote(enrollmentIntervalValue(interval)) + `; else printf '%s\n' 'Automatic background synchronization is currently supported only on macOS.' >&2; fi`
	}
	return command + " && . " + aliasesPath
}

func enrollmentIntervalValue(interval time.Duration) string {
	for _, preset := range enrollmentFrequencyPresets(interval) {
		if preset.Interval == interval {
			return preset.Value
		}
	}
	return interval.String()
}

func (a *webapp) watchInterval() time.Duration {
	if watchconfig.ValidateEnrollment(a.enrollmentWatchInterval) != nil {
		return watchconfig.DefaultInterval
	}
	return a.enrollmentWatchInterval
}

// handleDevicesMintToken is the "copy the commands" flow's entire
// backend: mint a single-use enrollment token against the same
// store.TokenRepo internal/api/auth.go's handleEnrollmentTokensCreate
// writes to, and render the one-flow command the new machine needs.
//
// The URL uses the explicit public origin when configured. Otherwise it is
// derived from the direct request (scheme + r.Host); forwarding headers are
// never trusted.
func (a *webapp) handleDevicesMintToken(w http.ResponseWriter, r *http.Request) {
	subject, ok := subjectFromContext(r.Context())
	if !ok {
		http.Error(w, translate(requestLanguage(r), "error.enrollment_mint"), http.StatusInternalServerError)
		return
	}

	minted, err := auth.Mint(store.TokenKindEnrollment)
	if err != nil {
		http.Error(w, translate(requestLanguage(r), "error.enrollment_mint"), http.StatusInternalServerError)
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
		http.Error(w, translate(requestLanguage(r), "error.enrollment_mint"), http.StatusInternalServerError)
		return
	}

	baseURL := ""
	if a.publicURL != nil {
		baseURL = a.publicURL.String()
	} else {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		baseURL = scheme + "://" + r.Host
	}
	statusID := a.enrollments.create(subject.TokenID, minted.Lookup, expiresAt)
	autoSync := r.FormValue("autoSync") == "true"
	selectedInterval := resolveEnrollmentFrequency(r.FormValue("syncFrequency"), a.watchInterval())
	manualCommand := mintCommand(baseURL, minted.Wire, false, selectedInterval)
	frequencyCommands := make([]enrollmentFrequencyCommand, 0, len(watchconfig.EnrollmentIntervals))
	lang := requestLanguage(r)
	for _, preset := range enrollmentFrequencyPresetsForLanguage(selectedInterval, lang) {
		frequencyCommands = append(frequencyCommands, enrollmentFrequencyCommand{
			Value:   preset.Value,
			Command: mintCommand(baseURL, minted.Wire, true, preset.Interval),
		})
	}
	command := manualCommand
	if autoSync {
		command = mintCommand(baseURL, minted.Wire, true, selectedInterval)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.mintResult.ExecuteTemplate(w, "device_mint_result", mintResultData{
		pageData:          pageDataFor(r),
		Command:           command,
		ManualCommand:     manualCommand,
		FrequencyCommands: frequencyCommands,
		ExpiresAt:         expiresAt.Format("2006-01-02 15:04:05 MST"),
		StatusID:          statusID,
		Message:           translate(lang, "enroll.waiting"),
	})
}

type enrollmentStatusData struct {
	pageData
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
		a.renderEnrollmentStatus(w, r, "device_enrollment_expired", enrollmentStatusData{Message: translate(requestLanguage(r), "enroll.expired")})
		return
	}

	token, err := a.store.Tokens().ByLookup(r.Context(), state.lookup)
	if err != nil {
		http.Error(w, translate(requestLanguage(r), "error.enrollment_status"), http.StatusInternalServerError)
		return
	}

	if token.UsedAt.IsZero() || token.SubjectID == "" {
		a.renderEnrollmentStatus(w, r, "device_enrollment_pending", enrollmentStatusData{
			StatusID: r.PathValue("id"),
			Message:  translate(requestLanguage(r), "enroll.waiting"),
		})
		return
	}

	device, err := a.store.Devices().Get(r.Context(), token.SubjectID)
	if err != nil {
		http.Error(w, translate(requestLanguage(r), "error.enrollment_status"), http.StatusInternalServerError)
		return
	}
	if device.LastSyncAt == nil {
		a.renderEnrollmentStatus(w, r, "device_enrollment_pending", enrollmentStatusData{
			StatusID: r.PathValue("id"),
			Message:  translate(requestLanguage(r), "enroll.enrolled"),
		})
		return
	}

	w.Header().Set("HX-Redirect", "/devices")
	a.renderEnrollmentStatus(w, r, "device_enrollment_complete", enrollmentStatusData{Message: translate(requestLanguage(r), "enroll.complete")})
}

func (a *webapp) renderEnrollmentStatus(w http.ResponseWriter, r *http.Request, name string, data enrollmentStatusData) {
	data.pageData = pageDataFor(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.mintResult.ExecuteTemplate(w, name, data)
}

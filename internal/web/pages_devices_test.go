package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/angeltonio/aliasdeck/internal/store/sqlitestore"
)

func TestMintCommandIsSingleSafeFlow(t *testing.T) {
	got := mintCommand("https://aliasdeck.test", "TOKEN_VALUE")
	want := `aliasdeck init --yes --skip-initial-sync && aliasdeck register --url 'https://aliasdeck.test' --token 'TOKEN_VALUE' && aliasdeck sync && . "${ALIASDECK_HOME:-${XDG_CONFIG_HOME:-$HOME/.config}/aliasdeck}/aliases.${SHELL##*/}"`
	if got != want {
		t.Fatalf("mintCommand() = %q, want %q", got, want)
	}

	quoted := shellQuote("value'with-quote")
	if quoted != `'value'\''with-quote'` {
		t.Fatalf("shellQuote() = %q, want a safely escaped single-quoted value", quoted)
	}

	if got := mintCommand("https://aliasdeck.test/$(touch pwned)", "TOKEN_VALUE"); !strings.Contains(got, "--url 'https://aliasdeck.test/$(touch pwned)'") {
		t.Fatalf("mintCommand() did not quote URL shell syntax: %q", got)
	}
}

func TestMintResultRendersCopyableCommand(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates() returned an error: %v", err)
	}

	var rendered bytes.Buffer
	err = templates.mintResult.ExecuteTemplate(&rendered, "device_mint_result", mintResultData{
		Command:   mintCommand("https://aliasdeck.test", "TOKEN_VALUE"),
		ExpiresAt: "2030-01-01 00:00:00 UTC",
		StatusID:  "opaque-status-id",
		Message:   "Waiting for the new machine to enroll and complete its first sync…",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate() returned an error: %v", err)
	}

	output := rendered.String()
	command := `aliasdeck init --yes --skip-initial-sync &amp;&amp; aliasdeck register --url &#39;https://aliasdeck.test&#39; --token &#39;TOKEN_VALUE&#39; &amp;&amp; aliasdeck sync &amp;&amp; . &#34;${ALIASDECK_HOME:-${XDG_CONFIG_HOME:-$HOME/.config}/aliasdeck}/aliases.${SHELL##*/}&#34;`
	if !strings.Contains(output, command) {
		t.Fatalf("rendered mint result does not contain the safe command: %q", output)
	}
	if strings.Contains(output, "aliasdeck register --url") && strings.Contains(output, "\naliasdeck sync") {
		t.Fatal("rendered mint result still presents registration and sync as separate commands")
	}
	if !strings.Contains(output, `hx-get="/devices/add/status/opaque-status-id"`) {
		t.Fatalf("rendered mint result does not poll with the opaque status ID: %q", output)
	}
	if strings.Contains(output, `hx-get="/devices/add/status/ade_`) {
		t.Fatalf("rendered status URL contains an enrollment token: %q", output)
	}
}

func TestDeviceEnrollmentMintRetainsCommandAndBindsOpaqueStatusToBrowserSession(t *testing.T) {
	a, st, _ := newDeviceStatusTestApp(t)
	req := httptest.NewRequest(http.MethodPost, "/devices/add/token", nil)
	req.Host = "aliasdeck.test"
	req = req.WithContext(withSubject(req.Context(), webSubject{TokenID: "browser-session-a", OperatorID: "operator-a"}))
	rec := httptest.NewRecorder()

	a.handleDevicesMintToken(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mint status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "aliasdeck register --url") {
		t.Fatalf("mint response does not retain a copyable enrollment command: %q", rec.Body.String())
	}

	statusID := onlyEnrollmentStatusID(t, a)
	state, ok := a.enrollments.get(statusID, "browser-session-a")
	if !ok {
		t.Fatal("minted status ID is not bound to the browser session")
	}
	if _, err := st.Tokens().ByLookup(context.Background(), state.lookup); err != nil {
		t.Fatalf("minted enrollment token was not persisted: %v", err)
	}
	if _, ok := a.enrollments.get(statusID, "browser-session-b"); ok {
		t.Fatal("a different browser session can access the minted enrollment status")
	}
}

func TestDeviceEnrollmentStatusPendingPollsWithoutTokenSecret(t *testing.T) {
	a, st, now := newDeviceStatusTestApp(t)
	lookup := createEnrollmentToken(t, st, now)
	statusID := a.enrollments.create("browser-session-a", lookup, now.Add(enrollmentTokenTTL))

	rec := pollEnrollmentStatus(a, statusID, "browser-session-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("pending status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("pending status HX-Redirect = %q, want empty", got)
	}
	if !strings.Contains(rec.Body.String(), `hx-get="/devices/add/status/`+statusID+`"`) {
		t.Fatalf("pending response does not continue polling its opaque status ID: %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), lookup) {
		t.Fatalf("pending response leaks the enrollment token lookup: %q", rec.Body.String())
	}
}

func TestDeviceEnrollmentStatusRedirectsAfterFirstSync(t *testing.T) {
	a, st, now := newDeviceStatusTestApp(t)
	lookup := createEnrollmentToken(t, st, now)
	statusID := a.enrollments.create("browser-session-a", lookup, now.Add(enrollmentTokenTTL))

	device, err := st.Tokens().ConsumeEnrollment(context.Background(), lookup, domain.Device{
		Name: "new-machine", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh, ClientVersion: "test",
	})
	if err != nil {
		t.Fatalf("ConsumeEnrollment() returned an error: %v", err)
	}

	beforeSync := pollEnrollmentStatus(a, statusID, "browser-session-a")
	if got := beforeSync.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("status before first sync HX-Redirect = %q, want empty", got)
	}
	if !strings.Contains(beforeSync.Body.String(), "Device enrolled. Waiting for its first sync") {
		t.Fatalf("status before first sync did not remain pending: %q", beforeSync.Body.String())
	}

	if err := st.Devices().Touch(context.Background(), device.ID, domain.PlatformMacOS, domain.ShellZsh, now); err != nil {
		t.Fatalf("Touch() returned an error: %v", err)
	}
	completed := pollEnrollmentStatus(a, statusID, "browser-session-a")
	if got := completed.Header().Get("HX-Redirect"); got != "/devices" {
		t.Fatalf("completed status HX-Redirect = %q, want /devices", got)
	}
	if !strings.Contains(completed.Body.String(), "Device enrolled and synced. Redirecting") {
		t.Fatalf("completed response does not render completion state: %q", completed.Body.String())
	}
}

func TestDeviceEnrollmentStatusIsBoundToMintingBrowserSession(t *testing.T) {
	a, st, now := newDeviceStatusTestApp(t)
	lookup := createEnrollmentToken(t, st, now)
	statusID := a.enrollments.create("browser-session-a", lookup, now.Add(enrollmentTokenTTL))

	rec := pollEnrollmentStatus(a, statusID, "browser-session-b")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other browser session status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestClassifyDeviceFreshnessUsesPrototypeThresholds(t *testing.T) {
	now := time.Date(2030, time.January, 1, 12, 0, 0, 0, time.UTC)
	at := func(age time.Duration) *time.Time {
		t := now.Add(-age)
		return &t
	}

	tests := []struct {
		name                   string
		lastSeenAt, lastSyncAt *time.Time
		wantLabel, wantClass   string
	}{
		{name: "not synced", wantLabel: "Not synced", wantClass: "stale"},
		{name: "not seen", lastSyncAt: at(0), wantLabel: "Not seen", wantClass: "stale"},
		{name: "recent at freshness boundary", lastSeenAt: at(prototypeDeviceFreshWithin), lastSyncAt: at(prototypeDeviceFreshWithin), wantLabel: "Recent", wantClass: "ok"},
		{name: "delayed after freshness boundary", lastSeenAt: at(prototypeDeviceFreshWithin + time.Nanosecond), lastSyncAt: at(0), wantLabel: "Delayed", wantClass: "stale"},
		{name: "stale after stale boundary", lastSeenAt: at(prototypeDeviceStaleAfter + time.Nanosecond), lastSyncAt: at(0), wantLabel: "Stale", wantClass: "stale"},
		{name: "sync overdue after stale boundary", lastSeenAt: at(0), lastSyncAt: at(prototypeDeviceStaleAfter + time.Nanosecond), wantLabel: "Sync overdue", wantClass: "stale"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyDeviceFreshness(tt.lastSeenAt, tt.lastSyncAt, now)
			if got.label != tt.wantLabel || got.class != tt.wantClass {
				t.Fatalf("classifyDeviceFreshness() = (%q, %q), want (%q, %q)", got.label, got.class, tt.wantLabel, tt.wantClass)
			}
		})
	}
}

func TestDevicesPageDisplaysAccessibleFreshnessStatus(t *testing.T) {
	a, st, now := newDeviceStatusTestApp(t)
	lookup := createEnrollmentToken(t, st, now)
	device, err := st.Tokens().ConsumeEnrollment(context.Background(), lookup, domain.Device{
		Name: "fresh-machine", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh, ClientVersion: "test",
	})
	if err != nil {
		t.Fatalf("ConsumeEnrollment() returned an error: %v", err)
	}
	if err := st.Devices().Touch(context.Background(), device.ID, domain.PlatformMacOS, domain.ShellZsh, now.Add(-prototypeDeviceFreshWithin)); err != nil {
		t.Fatalf("Touch() returned an error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/devices", nil)
	rec := httptest.NewRecorder()
	a.handleDevicesPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("devices page status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<th>Status</th>") {
		t.Fatalf("devices page does not render a status column: %q", body)
	}
	if !strings.Contains(body, `aria-label="Recent: This device checked in and synced within the last 15 minutes."`) {
		t.Fatalf("devices page does not render an accessible recent status: %q", body)
	}
}

func TestDevicesPageRendersUTCFallbackTimestamps(t *testing.T) {
	tests := []struct {
		name                 string
		setup                func(t *testing.T, st store.Store, now time.Time)
		wantLastSeen         string
		wantLastSeenDateTime string
		wantLastSync         string
		wantLastSyncDateTime string
	}{
		{
			name: "UTC timestamp after heartbeat",
			setup: func(t *testing.T, st store.Store, now time.Time) {
				t.Helper()
				lookup := createEnrollmentToken(t, st, now)
				device, err := st.Tokens().ConsumeEnrollment(context.Background(), lookup, domain.Device{
					Name: "heartbeat-machine", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh, ClientVersion: "test",
				})
				if err != nil {
					t.Fatalf("ConsumeEnrollment() returned an error: %v", err)
				}
				if err := st.Devices().Touch(context.Background(), device.ID, domain.PlatformMacOS, domain.ShellZsh, now.Add(-time.Hour)); err != nil {
					t.Fatalf("Touch() returned an error: %v", err)
				}
				heartbeatAt := time.Date(2030, time.January, 1, 1, 2, 0, 0, time.FixedZone("UTC-3", -3*60*60))
				if err := st.Devices().Heartbeat(context.Background(), device.ID, heartbeatAt); err != nil {
					t.Fatalf("Heartbeat() returned an error: %v", err)
				}
			},
			wantLastSeen:         "2030-01-01 04:02 UTC",
			wantLastSeenDateTime: "2030-01-01T04:02:00Z",
			wantLastSync:         "2029-12-31 23:00 UTC",
			wantLastSyncDateTime: "2029-12-31T23:00:00Z",
		},
		{
			name: "never when no heartbeat exists",
			setup: func(t *testing.T, st store.Store, now time.Time) {
				t.Helper()
				lookup := createEnrollmentToken(t, st, now)
				if _, err := st.Tokens().ConsumeEnrollment(context.Background(), lookup, domain.Device{
					Name: "unseen-machine", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh, ClientVersion: "test",
				}); err != nil {
					t.Fatalf("ConsumeEnrollment() returned an error: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, st, now := newDeviceStatusTestApp(t)
			tt.setup(t, st, now)

			rec := httptest.NewRecorder()
			a.handleDevicesPage(rec, httptest.NewRequest(http.MethodGet, "/devices", nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("devices page status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, `<script defer src="/static/local-time.js"></script>`) {
				t.Fatalf("devices page does not load the local time formatter: %q", body)
			}
			if !strings.Contains(body, `<p id="timestamp-timezone" aria-live="polite">Timestamps are shown in UTC.</p>`) {
				t.Fatalf("devices page does not provide a visible UTC fallback label: %q", body)
			}
			if tt.wantLastSeen == "" {
				if !strings.Contains(body, `<span class="mono muted">never</span>`) {
					t.Fatalf("devices page does not render the Last seen never state: %q", body)
				}
				if !strings.Contains(body, `<span class="mono">never</span>`) {
					t.Fatalf("devices page does not render the Last sync never state: %q", body)
				}
				return
			}

			for _, want := range []struct {
				name, datetime, fallback string
			}{
				{name: "Last seen", datetime: tt.wantLastSeenDateTime, fallback: tt.wantLastSeen},
				{name: "Last sync", datetime: tt.wantLastSyncDateTime, fallback: tt.wantLastSync},
			} {
				t.Run(want.name, func(t *testing.T) {
					semanticTime := `<time class="mono` + `" datetime="` + want.datetime + `" data-local-time data-utc="` + want.fallback + `"`
					if want.name == "Last seen" {
						semanticTime = `<time class="mono muted" datetime="` + want.datetime + `" data-local-time data-utc="` + want.fallback + `"`
					}
					if !strings.Contains(body, semanticTime) {
						t.Fatalf("devices page does not render semantic %s datetime %q: %q", want.name, want.datetime, body)
					}
					if !strings.Contains(body, `title="UTC timestamp. Converted to your local browser time when JavaScript is enabled."`) {
						t.Fatalf("devices page does not explain the UTC fallback for %s: %q", want.name, body)
					}
					if !strings.Contains(body, `>`+want.fallback+`</time>`) {
						t.Fatalf("devices page does not retain UTC fallback %q for %s: %q", want.fallback, want.name, body)
					}
				})
			}
		})
	}
}

func newDeviceStatusTestApp(t *testing.T) (*webapp, *sqlitestore.SQLiteStore, time.Time) {
	t.Helper()
	st, err := sqlitestore.Open(context.Background(), filepath.Join(t.TempDir(), "aliasdeck.db"))
	if err != nil {
		t.Fatalf("sqlitestore.Open() returned an error: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates() returned an error: %v", err)
	}
	now := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)
	return &webapp{store: st, now: func() time.Time { return now }, tmpl: templates, enrollments: newEnrollmentTracker()}, st, now
}

func createEnrollmentToken(t *testing.T, st store.Store, now time.Time) string {
	t.Helper()
	minted, err := auth.Mint(store.TokenKindEnrollment)
	if err != nil {
		t.Fatalf("auth.Mint() returned an error: %v", err)
	}
	if err := st.Tokens().Create(context.Background(), store.Token{
		Kind: store.TokenKindEnrollment, Lookup: minted.Lookup, SecretHash: minted.SecretHash,
		CreatedAt: now, ExpiresAt: now.Add(enrollmentTokenTTL),
	}); err != nil {
		t.Fatalf("Tokens().Create() returned an error: %v", err)
	}
	return minted.Lookup
}

func pollEnrollmentStatus(a *webapp, statusID, sessionTokenID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/devices/add/status/"+statusID, nil)
	req.SetPathValue("id", statusID)
	req = req.WithContext(withSubject(req.Context(), webSubject{TokenID: sessionTokenID, OperatorID: "operator-a"}))
	rec := httptest.NewRecorder()
	a.handleDeviceEnrollmentStatus(rec, req)
	return rec
}

func onlyEnrollmentStatusID(t *testing.T, a *webapp) string {
	t.Helper()
	a.enrollments.mu.Lock()
	defer a.enrollments.mu.Unlock()
	if len(a.enrollments.states) != 1 {
		t.Fatalf("enrollment state count = %d, want 1", len(a.enrollments.states))
	}
	for id := range a.enrollments.states {
		return id
	}
	panic("unreachable")
}

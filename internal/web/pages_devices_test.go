package web

import (
	"bytes"
	"context"
	"io/fs"
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
	manual := mintCommand("https://aliasdeck.test", "TOKEN_VALUE", false, 5*time.Second)
	wantManual := `aliasdeck init --yes --skip-initial-sync && aliasdeck register --url 'https://aliasdeck.test' --token 'TOKEN_VALUE' && aliasdeck sync && . "${ALIASDECK_HOME:-${XDG_CONFIG_HOME:-$HOME/.config}/aliasdeck}/aliases.${SHELL##*/}"`
	if manual != wantManual {
		t.Fatalf("mintCommand(manual) = %q, want %q", manual, wantManual)
	}
	automatic := mintCommand("https://aliasdeck.test", "TOKEN_VALUE", true, 5*time.Second)
	for _, want := range []string{"aliasdeck agent install --interval '5s'", `if [ "$(uname -s)" = Darwin ]`, "supported only on macOS", `&& . "${ALIASDECK_HOME`} {
		if !strings.Contains(automatic, want) {
			t.Errorf("mintCommand(auto) missing %q: %q", want, automatic)
		}
	}
	if strings.Contains(manual, "agent install") {
		t.Fatalf("manual command unexpectedly enables the watcher: %q", manual)
	}

	quoted := shellQuote("value'with-quote")
	if quoted != `'value'\''with-quote'` {
		t.Fatalf("shellQuote() = %q, want a safely escaped single-quoted value", quoted)
	}

	if got := mintCommand("https://aliasdeck.test/$(touch pwned)", "TOKEN_VALUE", false, 30*time.Second); !strings.Contains(got, "--url 'https://aliasdeck.test/$(touch pwned)'") {
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
		Command:           mintCommand("https://aliasdeck.test", "TOKEN_VALUE", true, 5*time.Second),
		ManualCommand:     mintCommand("https://aliasdeck.test", "TOKEN_VALUE", false, 5*time.Second),
		FrequencyCommands: testFrequencyCommands("https://aliasdeck.test", "TOKEN_VALUE"),
		ExpiresAt:         "2030-01-01 00:00:00 UTC",
		StatusID:          "opaque-status-id",
		Message:           "Waiting for the new machine to enroll and complete its first sync…",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate() returned an error: %v", err)
	}

	output := rendered.String()
	for _, command := range []string{"aliasdeck agent install", "data-enrollment-frequency=", "data-manual-command="} {
		if !strings.Contains(output, command) {
			t.Fatalf("rendered mint result does not contain %q: %q", command, output)
		}
	}
	for _, interval := range []string{"5s", "30s", "1m", "5m"} {
		if !strings.Contains(output, "--interval &#39;"+interval+"&#39;") {
			t.Errorf("rendered mint result is missing the %s command variant", interval)
		}
	}
	if got := strings.Count(output, "data-enrollment-frequency="); got != 4 {
		t.Fatalf("frequency command variant count = %d, want 4", got)
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

func TestDevicesAddPageShowsRequiredChecklistAndOneRealChoice(t *testing.T) {
	a, _, _ := newDeviceStatusTestApp(t)
	rec := httptest.NewRecorder()
	a.handleDevicesAddPage(rec, httptest.NewRequest(http.MethodGet, "/devices/add", nil))
	body := rec.Body.String()
	for _, step := range []string{"Initialize AliasDeck", "Register the device", "Run the first alias sync", "Load the synced aliases"} {
		if !strings.Contains(body, step) {
			t.Errorf("required checklist missing %q", step)
		}
	}
	if got := strings.Count(body, `type="checkbox"`); got != 1 {
		t.Fatalf("checkbox count = %d, want exactly 1", got)
	}
	if !strings.Contains(body, `name="autoSync" value="true" checked`) {
		t.Fatal("automatic synchronization choice is not checked by default")
	}
	if got := strings.Count(body, "<select "); got != 1 {
		t.Fatalf("select count = %d, want exactly 1", got)
	}
	if !strings.Contains(body, `<label for="sync-frequency">Alias sync frequency</label>`) || !strings.Contains(body, `aria-describedby="sync-frequency-help"`) {
		t.Fatal("sync frequency select is not accessibly labelled and described")
	}
	for _, copy := range []string{
		"Sync aliases automatically",
		"Downloads alias changes in the background and keeps the device connection status up to date on macOS. Other platforms still complete setup without background startup.",
	} {
		if !strings.Contains(body, copy) {
			t.Errorf("automatic alias sync copy missing %q", copy)
		}
	}
	wantOptions := []string{
		`<option value="5s">5 seconds</option>`,
		`<option value="30s" selected>30 seconds</option>`,
		`<option value="1m">1 minute</option>`,
		`<option value="5m">5 minutes</option>`,
	}
	last := -1
	for _, option := range wantOptions {
		index := strings.Index(body, option)
		if index <= last {
			t.Fatalf("frequency option %q is missing or out of order: %q", option, body)
		}
		last = index
	}
	if !strings.Contains(body, "Shorter intervals use more requests and may use more battery.") {
		t.Fatal("frequency helper does not disclose the activity tradeoff")
	}
	if !strings.Contains(body, `<script defer src="/static/device-enrollment.js"></script>`) {
		t.Fatal("Add device page does not load enrollment interaction behavior")
	}
	if strings.Contains(body, "run these two commands") {
		t.Fatal("page retains misleading two-command copy")
	}
}

func TestDevicesAddPageUsesExplicitDevFrequencyDefault(t *testing.T) {
	a, _, _ := newDeviceStatusTestApp(t)
	a.enrollmentWatchInterval = 5 * time.Second
	rec := httptest.NewRecorder()
	a.handleDevicesAddPage(rec, httptest.NewRequest(http.MethodGet, "/devices/add", nil))
	if !strings.Contains(rec.Body.String(), `<option value="5s" selected>5 seconds</option>`) {
		t.Fatalf("dev Add device default is not 5s: %q", rec.Body.String())
	}
}

func TestResolveEnrollmentFrequencyAllowsOnlyUIPresets(t *testing.T) {
	tests := []struct {
		raw      string
		fallback time.Duration
		want     time.Duration
	}{
		{raw: "5s", fallback: 30 * time.Second, want: 5 * time.Second},
		{raw: "30s", fallback: 5 * time.Second, want: 30 * time.Second},
		{raw: "1m", fallback: 5 * time.Second, want: time.Minute},
		{raw: "5m", fallback: 5 * time.Second, want: 5 * time.Minute},
		{raw: "10s", fallback: 5 * time.Second, want: 5 * time.Second},
		{raw: "garbage", fallback: 30 * time.Second, want: 30 * time.Second},
		{raw: "", fallback: 10 * time.Second, want: 30 * time.Second},
	}
	for _, tt := range tests {
		if got := resolveEnrollmentFrequency(tt.raw, tt.fallback); got != tt.want {
			t.Errorf("resolveEnrollmentFrequency(%q, %s) = %s, want %s", tt.raw, tt.fallback, got, tt.want)
		}
	}
}

func TestDeviceEnrollmentMintRetainsCommandAndBindsOpaqueStatusToBrowserSession(t *testing.T) {
	a, st, _ := newDeviceStatusTestApp(t)
	a.enrollmentWatchInterval = 5 * time.Second
	req := httptest.NewRequest(http.MethodPost, "/devices/add/token", strings.NewReader("autoSync=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
	if !strings.Contains(rec.Body.String(), "aliasdeck agent install") {
		t.Fatalf("checked auto-sync choice did not enable the watcher: %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "--interval &#39;5s&#39;") {
		t.Fatalf("dev enrollment command did not persist the configured 5s interval: %q", rec.Body.String())
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

func TestDeviceEnrollmentMintTamperedFrequencyFallsBackWithoutExtraToken(t *testing.T) {
	a, _, _ := newDeviceStatusTestApp(t)
	a.enrollmentWatchInterval = 5 * time.Second
	req := httptest.NewRequest(http.MethodPost, "/devices/add/token", strings.NewReader("autoSync=true&syncFrequency=10s"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "aliasdeck.test"
	req = req.WithContext(withSubject(req.Context(), webSubject{TokenID: "browser-session-a", OperatorID: "operator-a"}))
	rec := httptest.NewRecorder()

	a.handleDevicesMintToken(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mint status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `id="mint-commands"`) || !strings.Contains(rec.Body.String(), `--interval &#39;5s&#39;`) {
		t.Fatalf("tampered enrollment frequency did not fall back to configured 5s: %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `--interval &#39;10s&#39;`) {
		t.Fatalf("tampered 10s enrollment frequency reached a generated command: %q", rec.Body.String())
	}
	onlyEnrollmentStatusID(t, a)
}

func TestEnrollmentScriptSwitchesVisibleCommandWithoutReminting(t *testing.T) {
	script, err := fs.ReadFile(staticFS, "static/device-enrollment.js")
	if err != nil {
		t.Fatalf("read enrollment script: %v", err)
	}
	body := string(script)
	for _, want := range []string{
		`document.addEventListener("change"`,
		`document.addEventListener("htmx:afterSwap"`,
		`command.textContent =`,
		`navigator.clipboard.writeText(command.innerText)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("enrollment script missing %q", want)
		}
	}
	for _, forbidden := range []string{"fetch(", "XMLHttpRequest", "hx-post"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("enrollment script can unexpectedly mint or request a token via %q", forbidden)
		}
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

func TestClassifyDeviceFreshnessUsesProductThresholds(t *testing.T) {
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
		{name: "recent at freshness boundary", lastSeenAt: at(deviceFreshWithin), lastSyncAt: at(deviceFreshWithin), wantLabel: "Recent", wantClass: "ok"},
		{name: "delayed after freshness boundary", lastSeenAt: at(deviceFreshWithin + time.Nanosecond), lastSyncAt: at(0), wantLabel: "Delayed", wantClass: "stale"},
		{name: "stale after stale boundary", lastSeenAt: at(deviceStaleAfter + time.Nanosecond), lastSyncAt: at(0), wantLabel: "Stale", wantClass: "stale"},
		{name: "sync overdue after stale boundary", lastSeenAt: at(0), lastSyncAt: at(deviceStaleAfter + time.Nanosecond), wantLabel: "Sync overdue", wantClass: "stale"},
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
	if err := st.Devices().Touch(context.Background(), device.ID, domain.PlatformMacOS, domain.ShellZsh, now.Add(-deviceFreshWithin)); err != nil {
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
			if !strings.Contains(body, `<p id="timestamp-timezone" aria-live="polite"`) || !strings.Contains(body, `>Timestamps are shown in UTC.</p>`) {
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

func testFrequencyCommands(url, token string) []enrollmentFrequencyCommand {
	presets := enrollmentFrequencyPresets(5 * time.Second)
	commands := make([]enrollmentFrequencyCommand, 0, len(presets))
	for _, preset := range presets {
		commands = append(commands, enrollmentFrequencyCommand{
			Value:   preset.Value,
			Command: mintCommand(url, token, true, preset.Interval),
		})
	}
	return commands
}

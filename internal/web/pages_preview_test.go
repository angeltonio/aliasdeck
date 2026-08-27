package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

func fetchPreview(t *testing.T, a *webapp, deviceID string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/devices/"+deviceID+"/preview", nil)
	req.SetPathValue("id", deviceID)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.handleDevicePreview(rec, authed(req))
	return rec
}

func createAlias(t *testing.T, st store.Store, al domain.Alias) domain.Alias {
	t.Helper()
	created, err := st.Aliases().Create(context.Background(), al)
	if err != nil {
		t.Fatalf("creating alias %q: %v", al.Name, err)
	}
	return created
}

// TestPreviewReportsEveryAliasWithTheDimensionThatExcludedIt is the whole
// point of the page. An alias that never arrives is invisible from both the
// alias list and the device list, so the preview has to name the cause rather
// than only listing what survives.
func TestPreviewReportsEveryAliasWithTheDimensionThatExcludedIt(t *testing.T) {
	a, st := newAliasTestApp(t)
	laptops := seedProfile(t, st, "laptops")
	servers := seedProfile(t, st, "servers")
	dev := enrollDevice(t, a, st, "work-mac", laptops.ID)
	// alias_devices has a real foreign key, so pinning an alias to "another
	// machine" needs another machine to exist.
	other := enrollDevice(t, a, st, "someone-elses-mac")

	createAlias(t, st, domain.Alias{Name: "everywhere", Command: "echo 1", Enabled: true})
	createAlias(t, st, domain.Alias{Name: "off", Command: "echo 2"})
	createAlias(t, st, domain.Alias{Name: "linuxonly", Command: "echo 3", Enabled: true, Platforms: []domain.Platform{domain.PlatformLinux}})
	createAlias(t, st, domain.Alias{Name: "bashonly", Command: "echo 4", Enabled: true, Shells: []domain.Shell{domain.ShellBash}})
	createAlias(t, st, domain.Alias{Name: "serversonly", Command: "echo 5", Enabled: true, ProfileIDs: []string{servers.ID}})
	createAlias(t, st, domain.Alias{Name: "othermachine", Command: "echo 6", Enabled: true, DeviceIDs: []string{other.ID}})
	createAlias(t, st, domain.Alias{Name: "mine", Command: "echo 7", Enabled: true, ProfileIDs: []string{laptops.ID}})

	rec := fetchPreview(t, a, dev.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// Two aliases reach this device: the unconditional one and the one aimed
	// at the group it belongs to.
	if !strings.Contains(body, "This device receives 2 of them.") {
		t.Fatalf("preview does not report the count:\n%s", body)
	}

	for _, want := range []string{
		"the alias is disabled",
		"not targeted at macos",
		"not targeted at zsh",
		"this device is in none of its groups",
		"pinned to other devices",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("preview does not explain %q:\n%s", want, body)
		}
	}

	// Every alias is listed, excluded ones included — a preview that hid
	// them would answer the easy half of the question only.
	for _, name := range []string{"everywhere", "off", "linuxonly", "bashonly", "serversonly", "othermachine", "mine"} {
		if !strings.Contains(body, ">"+name+"<") {
			t.Fatalf("preview omits alias %q:\n%s", name, body)
		}
	}
}

// TestPreviewCountMatchesWhatSyncWouldResolve ties the page to the real
// resolution path. If the preview and the sync ever disagree, the page is
// worse than nothing: it would confidently explain the wrong answer.
func TestPreviewCountMatchesWhatSyncWouldResolve(t *testing.T) {
	a, st := newAliasTestApp(t)
	laptops := seedProfile(t, st, "laptops")
	dev := enrollDevice(t, a, st, "work-mac", laptops.ID)

	createAlias(t, st, domain.Alias{Name: "a", Command: "echo a", Enabled: true})
	createAlias(t, st, domain.Alias{Name: "b", Command: "echo b", Enabled: true, Platforms: []domain.Platform{domain.PlatformMacOS}})
	createAlias(t, st, domain.Alias{Name: "c", Command: "echo c", Enabled: true, Shells: []domain.Shell{domain.ShellBash}})
	createAlias(t, st, domain.Alias{Name: "d", Command: "echo d"})

	stored, err := st.Aliases().List(context.Background())
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	resolved := domain.Resolve(dev, stored)

	rec := fetchPreview(t, a, dev.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), formatted(languageEnglish, "preview.count", len(resolved.Aliases))) {
		t.Fatalf("preview count disagrees with domain.Resolve (%d aliases):\n%s", len(resolved.Aliases), rec.Body.String())
	}
}

func TestPreviewIsReadOnlyAndDoesNotStampTheDevice(t *testing.T) {
	a, st := newAliasTestApp(t)
	dev := enrollDevice(t, a, st, "work-mac")
	createAlias(t, st, domain.Alias{Name: "a", Command: "echo a", Enabled: true})

	before, err := st.Devices().Get(context.Background(), dev.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}

	if rec := fetchPreview(t, a, dev.ID, nil); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	after, err := st.Devices().Get(context.Background(), dev.ID)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	// A preview must not look like a sync: LastSyncAt and LastSeenAt are how
	// an operator judges whether a machine is healthy.
	if after.LastSyncAt != before.LastSyncAt || after.LastSeenAt != before.LastSeenAt {
		t.Fatal("the preview stamped the device's sync bookkeeping")
	}
	if after.Name != before.Name {
		t.Fatal("the preview changed the device")
	}
}

func TestPreviewOnAMissingDeviceIsNotFound(t *testing.T) {
	a, _ := newAliasTestApp(t)

	if rec := fetchPreview(t, a, "does-not-exist", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPreviewWithNoAliasesSaysSoRatherThanShowingAnEmptyTable(t *testing.T) {
	a, st := newAliasTestApp(t)
	dev := enrollDevice(t, a, st, "work-mac")

	rec := fetchPreview(t, a, dev.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No aliases exist on this server yet.") {
		t.Fatalf("preview does not report an empty server:\n%s", rec.Body.String())
	}
}

func TestPreviewRendersInSpanish(t *testing.T) {
	a, st := newAliasTestApp(t)
	dev := enrollDevice(t, a, st, "work-mac")
	createAlias(t, st, domain.Alias{Name: "off", Command: "echo 1"})

	rec := fetchPreview(t, a, dev.ID, map[string]string{"Accept-Language": "es"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "el alias está desactivado") {
		t.Fatalf("the Spanish reason is missing:\n%s", body)
	}
	if !strings.Contains(body, "Este dispositivo recibe 0 de ellos.") {
		t.Fatalf("the Spanish count is missing:\n%s", body)
	}
}

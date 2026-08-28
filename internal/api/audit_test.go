package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// TestEveryOperatorMutationRecordsAnAuditEvent is a structural check rather
// than a behavioural one, and that is the point: a behavioural test only
// covers the handlers someone remembered to write a case for, while the
// failure this guards against is a *new* mutation shipping without a record.
// An audit log is trusted for the action it missed.
//
// It reads this package's own source and requires every handler registered in
// routes() as a session-guarded write to contain an a.audit call.
func TestEveryOperatorMutationRecordsAnAuditEvent(t *testing.T) {
	routerSrc := readPackageFile(t, "router.go")

	// Handlers registered for POST/PUT/DELETE behind a session token are
	// operator mutations by definition — that is what the route table means.
	routeLine := regexp.MustCompile(`Method: http\.Method(?:Post|Put|Delete), Pattern: \w+, Handler: a\.(\w+), RequiredKind: store\.TokenKindSession`)

	var mutations []string
	for _, m := range routeLine.FindAllStringSubmatch(routerSrc, -1) {
		mutations = append(mutations, m[1])
	}
	if len(mutations) < 10 {
		t.Fatalf("found only %d session-guarded mutations (%v); the pattern above has stopped matching the route table", len(mutations), mutations)
	}

	// Logging out revokes the caller's own session. It changes no managed
	// data and has no subject an operator would ever search for, so it is
	// excluded deliberately rather than by omission.
	exempt := map[string]string{
		"handleLogout": "revokes the caller's own session; not a change to managed data",
	}

	sources := map[string]string{}
	for _, name := range []string{"aliases.go", "profiles.go", "devices.go", "auth.go"} {
		sources[name] = readPackageFile(t, name)
	}

	for _, handler := range mutations {
		if reason, ok := exempt[handler]; ok {
			t.Logf("%s exempt: %s", handler, reason)
			continue
		}

		body, file := handlerBody(t, sources, handler)
		if !strings.Contains(body, "a.audit(") {
			t.Errorf("%s (%s) performs an operator mutation but records no audit event", handler, file)
		}
	}
}

func readPackageFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(".", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(data)
}

// handlerBody returns the source of one handler function, from its signature
// to the closing brace in column zero.
func handlerBody(t *testing.T, sources map[string]string, handler string) (string, string) {
	t.Helper()
	needle := "func (a *api) " + handler + "("
	for file, src := range sources {
		i := strings.Index(src, needle)
		if i < 0 {
			continue
		}
		rest := src[i:]
		if end := strings.Index(rest, "\n}\n"); end >= 0 {
			return rest[:end], file
		}
		return rest, file
	}
	t.Fatalf("could not find %s in any inspected file", handler)
	return "", ""
}

// TestAuditRecordsTheOperatorBehindAnAPIMutation proves the structural check
// above is guarding something real: the recorded actor is the session's
// operator, with the username it held at the time.
func TestAuditRecordsTheOperatorBehindAnAPIMutation(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	body, _ := json.Marshal(domain.Alias{Name: "gs", Command: "git status", Enabled: true})
	rec := doRequest(h, http.MethodPost, "/api/v1/aliases", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	events, err := s.Audit().Recent(context.Background(), 10)
	if err != nil {
		t.Fatalf("Recent(): %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want one: %+v", len(events), events)
	}
	e := events[0]
	if e.Action != store.AuditAliasCreated {
		t.Errorf("Action = %q, want %q", e.Action, store.AuditAliasCreated)
	}
	if e.ActorID != opID || e.ActorName != "admin" {
		t.Errorf("actor = %s/%s, want %s/admin", e.ActorID, e.ActorName, opID)
	}
	if e.SubjectLabel != "gs" {
		t.Errorf("SubjectLabel = %q, want gs", e.SubjectLabel)
	}
}

// TestAuditRecordsWhatWasDeletedNotJustThatSomethingWas covers the case the
// denormalized label exists for: once the row is gone there is nothing left
// to resolve a name from.
func TestAuditRecordsWhatWasDeletedNotJustThatSomethingWas(t *testing.T) {
	s, opID := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	token := mintSessionFor(s, opID)
	h := newTestRouter(t, s)

	created, err := s.Aliases().Create(context.Background(), domain.Alias{Name: "doomed", Command: "echo 1", Enabled: true})
	if err != nil {
		t.Fatalf("seeding alias: %v", err)
	}

	rec := doRequest(h, http.MethodDelete, "/api/v1/aliases/"+created.ID, token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	events, _ := s.Audit().Recent(context.Background(), 10)
	if len(events) != 1 || events[0].SubjectLabel != "doomed" {
		t.Fatalf("events = %+v, want one naming the deleted alias", events)
	}
}

// TestSyncAndHeartbeatRecordNothing is the exclusion that justifies putting
// the write points in handlers rather than in the store. These run every few
// seconds per device; recording them would bury every row worth reading.
func TestSyncAndHeartbeatRecordNothing(t *testing.T) {
	s, _ := newFakeStoreWithOperator("admin", "irrelevant-for-this-test")
	h := newTestRouter(t, s)
	enrollment := mintEnrollmentToken(s, nil, time.Now().Add(time.Hour))

	body, _ := json.Marshal(map[string]string{"name": "laptop", "platform": "macos", "shell": "zsh"})
	reg := doRequest(h, http.MethodPost, "/api/v1/devices/register", enrollment, body)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want 201: %s", reg.Code, reg.Body.String())
	}
	var registered deviceTokenResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &registered); err != nil {
		t.Fatalf("decoding registration: %v", err)
	}

	// Registration is a device action too — the device presents an
	// enrollment token, not an operator session — so the count starts here
	// rather than at zero for the whole test.
	before, _ := s.Audit().Count(context.Background())

	if rec := doRequest(h, http.MethodGet, "/api/v1/sync?platform=macos&shell=zsh", registered.DeviceToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("sync status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(h, http.MethodPost, "/api/v1/heartbeat", registered.DeviceToken, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("heartbeat status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	after, _ := s.Audit().Count(context.Background())
	if after != before {
		events, _ := s.Audit().Recent(context.Background(), 10)
		t.Fatalf("device traffic recorded %d new events: %+v", after-before, events)
	}
}

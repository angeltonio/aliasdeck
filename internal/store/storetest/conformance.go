// Package storetest is the backend conformance suite for internal/store.
//
// It is written only against store's interfaces (server-persistence spec,
// "Backend Conformance Suite Is Interface-Only"): any future backend —
// PostgreSQL or otherwise — proves itself compliant by passing Run, with
// zero changes to this file.
package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// Run exercises every backend-agnostic guarantee store's interfaces make:
// CRUD fidelity, ErrNotFound/ErrConflict, cascade behavior on delete,
// deterministic list ordering, a cancelled context writing nothing, and
// ConsumeEnrollment's exactly-one-device guarantee under concurrent
// callers.
//
// newStore must return a Store backed by a freshly migrated, empty
// database. Run calls it once per subtest so each subtest starts from a
// clean slate regardless of execution order, and subtests may run with
// t.Parallel() by the caller's newStore without interfering with each
// other.
func Run(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()

	t.Run("AliasCRUD", func(t *testing.T) { testAliasCRUD(t, newStore(t)) })
	t.Run("AliasNotFoundAndConflict", func(t *testing.T) { testAliasNotFoundAndConflict(t, newStore(t)) })
	t.Run("AliasListOrdering", func(t *testing.T) { testAliasListOrdering(t, newStore(t)) })
	t.Run("AliasCascadeDeletesTargeting", func(t *testing.T) { testAliasCascadeDeletesTargeting(t, newStore(t)) })
	t.Run("AliasNameWithSQLMetacharactersRoundTrips", func(t *testing.T) { testAliasNameWithSQLMetacharactersRoundTrips(t, newStore(t)) })
	t.Run("ProfileCRUD", func(t *testing.T) { testProfileCRUD(t, newStore(t)) })
	t.Run("OperatorCRUD", func(t *testing.T) { testOperatorCRUD(t, newStore(t)) })
	t.Run("TokenLifecycle", func(t *testing.T) { testTokenLifecycle(t, newStore(t)) })
	t.Run("DeviceLifecycle", func(t *testing.T) { testDeviceLifecycle(t, newStore(t)) })
	t.Run("ConsumeEnrollmentIsAtomic", func(t *testing.T) { testConsumeEnrollmentIsAtomic(t, newStore(t)) })
	t.Run("CancelledContextWritesNothing", func(t *testing.T) { testCancelledContextWritesNothing(t, newStore(t)) })
}

func testAliasCRUD(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	repo := st.Aliases()

	created, err := repo.Create(ctx, domain.Alias{
		Name:      "dcu",
		Command:   "docker compose up -d",
		Enabled:   true,
		Platforms: []domain.Platform{domain.PlatformLinux},
	})
	if err != nil {
		t.Fatalf("Create() = %v, want nil error", err)
	}
	if created.ID == "" {
		t.Fatal("Create() left ID empty")
	}
	if created.Name != "dcu" || created.Command != "docker compose up -d" {
		t.Fatalf("Create() returned %+v, want name/command preserved", created)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get(%q) = %v, want nil error", created.ID, err)
	}
	if got.Name != "dcu" || len(got.Platforms) != 1 || got.Platforms[0] != domain.PlatformLinux {
		t.Fatalf("Get(%q) = %+v, want fields matching Create() exactly", created.ID, got)
	}

	got.Command = "docker compose up -d --build"
	got.Description = "start the stack"
	updated, err := repo.Update(ctx, got)
	if err != nil {
		t.Fatalf("Update() = %v, want nil error", err)
	}
	if updated.Command != "docker compose up -d --build" || updated.Description != "start the stack" {
		t.Fatalf("Update() = %+v, want the new command/description persisted", updated)
	}

	roundTripped, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() after Update(): %v", err)
	}
	if roundTripped.Command != "docker compose up -d --build" {
		t.Fatalf("Get() after Update() = %+v, want the update to have persisted", roundTripped)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() = %v, want nil error", err)
	}
	if _, err := repo.Get(ctx, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get() after Delete() = %v, want ErrNotFound", err)
	}
}

func testAliasNotFoundAndConflict(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	repo := st.Aliases()

	if _, err := repo.Get(ctx, "does-not-exist"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get() on a missing id = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, "does-not-exist"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete() on a missing id = %v, want ErrNotFound", err)
	}
	if _, err := repo.Update(ctx, domain.Alias{ID: "does-not-exist", Name: "x", Command: "true"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Update() on a missing id = %v, want ErrNotFound", err)
	}

	if _, err := repo.Create(ctx, domain.Alias{Name: "dup", Command: "true", Enabled: true}); err != nil {
		t.Fatalf("first Create(): %v", err)
	}
	if _, err := repo.Create(ctx, domain.Alias{Name: "dup", Command: "false", Enabled: true}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Create() with a duplicate name = %v, want ErrConflict", err)
	}
}

func testAliasListOrdering(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	repo := st.Aliases()

	for _, name := range []string{"zsh-alias", "alpha", "middle"} {
		if _, err := repo.Create(ctx, domain.Alias{Name: name, Command: "true", Enabled: true}); err != nil {
			t.Fatalf("seeding alias %q: %v", name, err)
		}
	}

	first, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	second, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() second call: %v", err)
	}

	if len(first) != 3 {
		t.Fatalf("List() returned %d aliases, want 3", len(first))
	}
	wantOrder := []string{"alpha", "middle", "zsh-alias"}
	for i, a := range first {
		if a.Name != wantOrder[i] {
			t.Fatalf("List()[%d].Name = %q, want %q — ordering must be deterministic by name", i, a.Name, wantOrder[i])
		}
	}
	for i := range first {
		if first[i].Name != second[i].Name {
			t.Fatalf("List() returned a different order on a repeat call with no writes in between: %q vs %q", first[i].Name, second[i].Name)
		}
	}
}

func testAliasCascadeDeletesTargeting(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()

	profile, err := st.Profiles().Create(ctx, domain.Profile{Name: "development"})
	if err != nil {
		t.Fatalf("creating profile: %v", err)
	}

	alias, err := st.Aliases().Create(ctx, domain.Alias{
		Name: "dcu", Command: "true", Enabled: true, ProfileIDs: []string{profile.ID},
	})
	if err != nil {
		t.Fatalf("creating alias: %v", err)
	}
	if len(alias.ProfileIDs) != 1 || alias.ProfileIDs[0] != profile.ID {
		t.Fatalf("Create() = %+v, want ProfileIDs to include %q", alias, profile.ID)
	}

	if err := st.Profiles().Delete(ctx, profile.ID); err != nil {
		t.Fatalf("deleting profile: %v", err)
	}

	got, err := st.Aliases().Get(ctx, alias.ID)
	if err != nil {
		t.Fatalf("Get() alias after deleting its profile: %v", err)
	}
	if len(got.ProfileIDs) != 0 {
		t.Fatalf("alias.ProfileIDs = %v after its profile was deleted, want empty — the join row must cascade away", got.ProfileIDs)
	}
}

// testAliasNameWithSQLMetacharactersRoundTrips is the threat-matrix "SQL
// construction" case: a name built to look like a SQL statement must
// survive as literal text, proving writes go through parameterized
// queries rather than string concatenation.
func testAliasNameWithSQLMetacharactersRoundTrips(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	repo := st.Aliases()

	const hostile = `x'; DROP TABLE aliases; --`
	created, err := repo.Create(ctx, domain.Alias{Name: hostile, Command: "true", Enabled: true})
	if err != nil {
		t.Fatalf("Create() with a SQL-metacharacter name = %v, want nil error", err)
	}
	if created.Name != hostile {
		t.Fatalf("Create() returned Name = %q, want %q preserved literally", created.Name, hostile)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() after storing a hostile name: %v", err)
	}
	if got.Name != hostile {
		t.Fatalf("Get().Name = %q, want %q — the aliases table must still exist and the name must round-trip verbatim", got.Name, hostile)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() after storing a hostile name: %v — a real injection would have dropped the table this call reads", err)
	}
	if len(list) != 1 {
		t.Fatalf("List() returned %d aliases, want 1 — the aliases table must be intact", len(list))
	}
}

func testProfileCRUD(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	repo := st.Profiles()

	created, err := repo.Create(ctx, domain.Profile{Name: "homelab", Description: "home infra"})
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create() left ID empty")
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil || got.Name != "homelab" || got.Description != "home infra" {
		t.Fatalf("Get() = %+v, %v, want the created fields", got, err)
	}

	got.Description = "home lab, updated"
	updated, err := repo.Update(ctx, got)
	if err != nil || updated.Description != "home lab, updated" {
		t.Fatalf("Update() = %+v, %v", updated, err)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if _, err := repo.Get(ctx, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get() after Delete() = %v, want ErrNotFound", err)
	}
}

func testOperatorCRUD(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	repo := st.Operators()

	if n, err := repo.Count(ctx); err != nil || n != 0 {
		t.Fatalf("Count() on an empty store = %d, %v, want 0, nil", n, err)
	}

	created, err := repo.Create(ctx, store.Operator{Username: "root", PasswordHash: []byte("hash")})
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create() left ID empty")
	}

	if n, err := repo.Count(ctx); err != nil || n != 1 {
		t.Fatalf("Count() after one Create() = %d, %v, want 1, nil", n, err)
	}

	byName, err := repo.ByUsername(ctx, "root")
	if err != nil || byName.ID != created.ID {
		t.Fatalf("ByUsername(%q) = %+v, %v, want the created operator", "root", byName, err)
	}

	if _, err := repo.Create(ctx, store.Operator{Username: "root", PasswordHash: []byte("other")}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Create() with a duplicate username = %v, want ErrConflict", err)
	}

	if _, err := repo.ByUsername(ctx, "nobody"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ByUsername() on a missing username = %v, want ErrNotFound", err)
	}
	if _, err := repo.Get(ctx, "does-not-exist"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get() on a missing id = %v, want ErrNotFound", err)
	}
}

func testTokenLifecycle(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	repo := st.Tokens()

	tok := store.Token{
		Lookup:     "lookup-1",
		Kind:       store.TokenKindSession,
		SubjectID:  "operator-1",
		SecretHash: []byte("secret-hash"),
		CreatedAt:  time.Now().UTC(),
		ExpiresAt:  time.Now().UTC().Add(24 * time.Hour),
	}
	if err := repo.Create(ctx, tok); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	got, err := repo.ByLookup(ctx, "lookup-1")
	if err != nil {
		t.Fatalf("ByLookup(): %v", err)
	}
	if got.Kind != store.TokenKindSession || got.SubjectID != "operator-1" || string(got.SecretHash) != "secret-hash" {
		t.Fatalf("ByLookup() = %+v, want the created token's fields", got)
	}
	if got.ID == "" {
		t.Fatal("ByLookup() returned a token with an empty ID")
	}

	if _, err := repo.ByLookup(ctx, "does-not-exist"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ByLookup() on a missing lookup = %v, want ErrNotFound", err)
	}

	now := time.Now().UTC()
	if err := repo.Revoke(ctx, got.ID, now); err != nil {
		t.Fatalf("Revoke(): %v", err)
	}
	revoked, err := repo.ByLookup(ctx, "lookup-1")
	if err != nil || revoked.RevokedAt.IsZero() {
		t.Fatalf("ByLookup() after Revoke() = %+v, %v, want a non-zero RevokedAt", revoked, err)
	}

	if err := repo.Revoke(ctx, "does-not-exist", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Revoke() on a missing id = %v, want ErrNotFound", err)
	}

	// RevokeSubject: two device tokens for the same subject, one call
	// revokes both — "log out everywhere" for a subject (design decision 8).
	for _, lookup := range []string{"device-a", "device-b"} {
		if err := repo.Create(ctx, store.Token{
			Lookup: lookup, Kind: store.TokenKindDevice, SubjectID: "device-1",
			SecretHash: []byte("hash"), CreatedAt: now,
		}); err != nil {
			t.Fatalf("creating token %q: %v", lookup, err)
		}
	}
	if err := repo.RevokeSubject(ctx, store.TokenKindDevice, "device-1", now); err != nil {
		t.Fatalf("RevokeSubject(): %v", err)
	}
	for _, lookup := range []string{"device-a", "device-b"} {
		tok, err := repo.ByLookup(ctx, lookup)
		if err != nil || tok.RevokedAt.IsZero() {
			t.Fatalf("token %q after RevokeSubject() = %+v, %v, want RevokedAt set", lookup, tok, err)
		}
	}
}

func testDeviceLifecycle(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()

	if err := st.Tokens().Create(ctx, store.Token{
		Lookup: "enroll-1", Kind: store.TokenKindEnrollment, SecretHash: []byte("hash"), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("creating enrollment token: %v", err)
	}

	dev, err := st.Tokens().ConsumeEnrollment(ctx, "enroll-1", domain.Device{
		Name: "macbook", Platform: domain.PlatformMacOS, Shell: domain.ShellZsh,
	})
	if err != nil {
		t.Fatalf("ConsumeEnrollment(): %v", err)
	}
	if dev.ID == "" {
		t.Fatal("ConsumeEnrollment() left device ID empty")
	}

	got, err := st.Devices().Get(ctx, dev.ID)
	if err != nil || got.Name != "macbook" || got.Platform != domain.PlatformMacOS {
		t.Fatalf("Devices().Get() = %+v, %v, want the enrolled device", got, err)
	}

	list, err := st.Devices().List(ctx)
	if err != nil || len(list) != 1 || list[0].ID != dev.ID {
		t.Fatalf("Devices().List() = %+v, %v, want exactly the enrolled device", list, err)
	}

	got.Name = "macbook-pro"
	updated, err := st.Devices().Update(ctx, got)
	if err != nil || updated.Name != "macbook-pro" {
		t.Fatalf("Devices().Update() = %+v, %v", updated, err)
	}

	touchedAt := time.Now().UTC().Truncate(time.Second)
	if err := st.Devices().Touch(ctx, dev.ID, domain.PlatformLinux, domain.ShellBash, touchedAt); err != nil {
		t.Fatalf("Devices().Touch(): %v", err)
	}
	afterTouch, err := st.Devices().Get(ctx, dev.ID)
	if err != nil {
		t.Fatalf("Get() after Touch(): %v", err)
	}
	if afterTouch.Platform != domain.PlatformLinux || afterTouch.Shell != domain.ShellBash {
		t.Fatalf("Get() after Touch() = %+v, want platform/shell overwritten by the sync report", afterTouch)
	}
	if afterTouch.LastSeenAt == nil || !afterTouch.LastSeenAt.Equal(touchedAt) {
		t.Fatalf("Get() after Touch() LastSeenAt = %v, want %v", afterTouch.LastSeenAt, touchedAt)
	}
	if afterTouch.LastSyncAt == nil || !afterTouch.LastSyncAt.Equal(touchedAt) {
		t.Fatalf("Get() after Touch() LastSyncAt = %v, want %v", afterTouch.LastSyncAt, touchedAt)
	}

	revokedAt := time.Now().UTC()
	if err := st.Devices().Revoke(ctx, dev.ID, revokedAt); err != nil {
		t.Fatalf("Devices().Revoke(): %v", err)
	}

	if err := st.Devices().Delete(ctx, dev.ID); err != nil {
		t.Fatalf("Devices().Delete(): %v", err)
	}
	if _, err := st.Devices().Get(ctx, dev.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get() after Delete() = %v, want ErrNotFound", err)
	}

	if _, err := st.Devices().Get(ctx, "does-not-exist"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get() on a missing id = %v, want ErrNotFound", err)
	}
	if _, err := st.Devices().Update(ctx, domain.Device{ID: "does-not-exist", Name: "x"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Update() on a missing id = %v, want ErrNotFound", err)
	}
	if err := st.Devices().Touch(ctx, "does-not-exist", domain.PlatformLinux, domain.ShellBash, time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Touch() on a missing id = %v, want ErrNotFound", err)
	}
	if err := st.Devices().Revoke(ctx, "does-not-exist", time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Revoke() on a missing id = %v, want ErrNotFound", err)
	}
	if err := st.Devices().Delete(ctx, "does-not-exist"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete() on a missing id = %v, want ErrNotFound", err)
	}
}

// testConsumeEnrollmentIsAtomic proves the conformance suite's hardest
// guarantee: two concurrent ConsumeEnrollment calls against the same
// enrollment token yield exactly one device, never two, regardless of how
// many callers race.
func testConsumeEnrollmentIsAtomic(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()

	if err := st.Tokens().Create(ctx, store.Token{
		Lookup: "race-enroll", Kind: store.TokenKindEnrollment, SecretHash: []byte("hash"), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("creating enrollment token: %v", err)
	}

	const racers = 8
	var wg sync.WaitGroup
	results := make([]error, racers)
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := st.Tokens().ConsumeEnrollment(ctx, "race-enroll", domain.Device{
				Name: "racer", Platform: domain.PlatformLinux, Shell: domain.ShellBash,
			})
			results[i] = err
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, store.ErrConflict):
			// expected for every loser
		default:
			t.Fatalf("ConsumeEnrollment() concurrent call returned %v, want nil or ErrConflict", err)
		}
	}
	if successes != 1 {
		t.Fatalf("ConsumeEnrollment() succeeded %d times out of %d concurrent callers, want exactly 1", successes, racers)
	}

	list, err := st.Devices().List(ctx)
	if err != nil {
		t.Fatalf("Devices().List(): %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("Devices().List() returned %d devices after %d racing enrollments, want exactly 1", len(list), racers)
	}
}

func testCancelledContextWritesNothing(t *testing.T, st store.Store) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := st.Aliases().Create(ctx, domain.Alias{Name: "cancelled", Command: "true", Enabled: true}); err == nil {
		t.Fatal("Create() with a cancelled context returned nil error, want an error")
	}

	list, err := st.Aliases().List(context.Background())
	if err != nil {
		t.Fatalf("List() after a cancelled write: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List() = %d aliases after a Create() with a cancelled context, want 0 — a cancelled write must write nothing", len(list))
	}
}

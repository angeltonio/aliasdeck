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
	t.Run("AliasCascadeDeletesDeviceTargeting", func(t *testing.T) { testAliasCascadeDeletesDeviceTargeting(t, newStore(t)) })
	t.Run("AliasNameWithSQLMetacharactersRoundTrips", func(t *testing.T) { testAliasNameWithSQLMetacharactersRoundTrips(t, newStore(t)) })
	t.Run("AliasInvalidReference", func(t *testing.T) { testAliasInvalidReference(t, newStore(t)) })
	t.Run("AliasCreateRollsBackFullyOnPartialTargetingFailure", func(t *testing.T) { testAliasCreateRollsBackFullyOnPartialTargetingFailure(t, newStore(t)) })
	t.Run("ProfileCRUD", func(t *testing.T) { testProfileCRUD(t, newStore(t)) })
	t.Run("ProfileNotFoundAndConflict", func(t *testing.T) { testProfileNotFoundAndConflict(t, newStore(t)) })
	t.Run("DeviceProfileMembershipCascadesOnProfileDelete", func(t *testing.T) { testDeviceProfileMembershipCascadesOnProfileDelete(t, newStore(t)) })
	t.Run("DeviceProfileMembershipCascadesOnDeviceDelete", func(t *testing.T) { testDeviceProfileMembershipCascadesOnDeviceDelete(t, newStore(t)) })
	t.Run("OperatorCRUD", func(t *testing.T) { testOperatorCRUD(t, newStore(t)) })
	t.Run("AuditAppendAndRecent", func(t *testing.T) { testAuditAppendAndRecent(t, newStore(t)) })
	t.Run("TokenLifecycle", func(t *testing.T) { testTokenLifecycle(t, newStore(t)) })
	t.Run("TokenLookupConflict", func(t *testing.T) { testTokenLookupConflict(t, newStore(t)) })
	t.Run("ConsumeEnrollmentRejectsExpiredToken", func(t *testing.T) { testConsumeEnrollmentRejectsExpiredToken(t, newStore(t)) })
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

	// Update() must reject a name collision with a DIFFERENT existing row,
	// not just report success trivially because no other row happens to
	// share the id being updated.
	other, err := repo.Create(ctx, domain.Alias{Name: "other", Command: "true", Enabled: true})
	if err != nil {
		t.Fatalf("creating a second alias: %v", err)
	}
	other.Name = "dup"
	if _, err := repo.Update(ctx, other); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Update() renaming a row to collide with a different row's name = %v, want ErrConflict", err)
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

// testAliasCascadeDeletesDeviceTargeting is testAliasCascadeDeletesTargeting's
// counterpart for alias_devices: the profile-delete case above does not
// exercise the device-delete cascade, so a migration edit dropping
// ON DELETE CASCADE from alias_devices specifically would go undetected
// without this.
func testAliasCascadeDeletesDeviceTargeting(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()

	if err := st.Tokens().Create(ctx, store.Token{
		Lookup: "cascade-alias-device-enroll", Kind: store.TokenKindEnrollment, SecretHash: []byte("hash"), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("creating enrollment token: %v", err)
	}
	dev, err := st.Tokens().ConsumeEnrollment(ctx, "cascade-alias-device-enroll", domain.Device{
		Name: "cascade-alias-device", Platform: domain.PlatformLinux, Shell: domain.ShellBash,
	})
	if err != nil {
		t.Fatalf("enrolling device: %v", err)
	}

	alias, err := st.Aliases().Create(ctx, domain.Alias{
		Name: "dcu-device-target", Command: "true", Enabled: true, DeviceIDs: []string{dev.ID},
	})
	if err != nil {
		t.Fatalf("creating alias: %v", err)
	}
	if len(alias.DeviceIDs) != 1 || alias.DeviceIDs[0] != dev.ID {
		t.Fatalf("Create() = %+v, want DeviceIDs to include %q", alias, dev.ID)
	}

	if err := st.Devices().Delete(ctx, dev.ID); err != nil {
		t.Fatalf("deleting device: %v", err)
	}

	got, err := st.Aliases().Get(ctx, alias.ID)
	if err != nil {
		t.Fatalf("Get() alias after deleting its targeted device: %v", err)
	}
	if len(got.DeviceIDs) != 0 {
		t.Fatalf("alias.DeviceIDs = %v after its targeted device was deleted, want empty — the join row must cascade away", got.DeviceIDs)
	}
}

// testAliasInvalidReference is the design decision 18 case: an alias
// naming a profile or device ID that does not exist must surface as
// ErrInvalidReference, distinct from ErrConflict (a name collision) —
// mapWriteError previously collapsed both to ErrConflict, which would have
// told a caller "that name is already taken" for a completely different
// failure.
func testAliasInvalidReference(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	repo := st.Aliases()

	_, err := repo.Create(ctx, domain.Alias{
		Name: "dangling-profile-ref", Command: "true", Enabled: true, ProfileIDs: []string{"does-not-exist"},
	})
	if !errors.Is(err, store.ErrInvalidReference) {
		t.Fatalf("Create() with a nonexistent ProfileIDs entry = %v, want ErrInvalidReference", err)
	}
	if errors.Is(err, store.ErrConflict) {
		t.Fatalf("Create() with a nonexistent ProfileIDs entry = %v, must not also satisfy errors.Is(err, ErrConflict) — a dangling reference is not a name collision", err)
	}

	_, err = repo.Create(ctx, domain.Alias{
		Name: "dangling-device-ref", Command: "true", Enabled: true, DeviceIDs: []string{"does-not-exist"},
	})
	if !errors.Is(err, store.ErrInvalidReference) {
		t.Fatalf("Create() with a nonexistent DeviceIDs entry = %v, want ErrInvalidReference", err)
	}

	created, err := repo.Create(ctx, domain.Alias{Name: "valid-first", Command: "true", Enabled: true})
	if err != nil {
		t.Fatalf("creating a valid alias: %v", err)
	}
	created.ProfileIDs = []string{"still-does-not-exist"}
	if _, err := repo.Update(ctx, created); !errors.Is(err, store.ErrInvalidReference) {
		t.Fatalf("Update() adding a nonexistent ProfileIDs entry = %v, want ErrInvalidReference", err)
	}
}

// testAliasCreateRollsBackFullyOnPartialTargetingFailure is the "fails
// partway through a multi-table write" case testCancelledContextWritesNothing
// does not cover: that test cancels its context before the call, so it
// dies in BeginTx and never reaches the multi-statement targeting write
// the transaction exists to protect. This test instead gives Create() one
// valid ProfileIDs entry (which inserts successfully) followed by one
// dangling entry (which fails), forcing a real failure partway through,
// and asserts neither the alias row nor the first targeting row survived.
func testAliasCreateRollsBackFullyOnPartialTargetingFailure(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()

	profile, err := st.Profiles().Create(ctx, domain.Profile{Name: "partial-write-profile"})
	if err != nil {
		t.Fatalf("creating profile: %v", err)
	}

	_, err = st.Aliases().Create(ctx, domain.Alias{
		Name: "partial-write-alias", Command: "true", Enabled: true,
		ProfileIDs: []string{profile.ID, "does-not-exist"},
	})
	if !errors.Is(err, store.ErrInvalidReference) {
		t.Fatalf("Create() with one valid and one dangling ProfileIDs entry = %v, want ErrInvalidReference", err)
	}

	aliases, err := st.Aliases().List(ctx)
	if err != nil {
		t.Fatalf("List() aliases after a failed multi-row targeting write: %v", err)
	}
	if len(aliases) != 0 {
		t.Fatalf("Aliases().List() = %d aliases after Create() failed partway through targeting, want 0 — a mid-transaction failure must leave no partial row", len(aliases))
	}

	profiles, err := st.Profiles().List(ctx)
	if err != nil {
		t.Fatalf("List() profiles: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("Profiles().List() = %d profiles, want 1 (only the one seeded before the failed alias write, untouched by its rollback)", len(profiles))
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

// testProfileNotFoundAndConflict is ProfileRepo's counterpart to
// testAliasNotFoundAndConflict: testProfileCRUD only exercises the happy
// path, so missing-id Update/Delete, a duplicate-name Create, and an
// Update() colliding with a different row's name were previously
// uncovered here even though ProfileRepo documents all three.
func testProfileNotFoundAndConflict(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	repo := st.Profiles()

	if _, err := repo.Get(ctx, "does-not-exist"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get() on a missing id = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, "does-not-exist"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete() on a missing id = %v, want ErrNotFound", err)
	}
	if _, err := repo.Update(ctx, domain.Profile{ID: "does-not-exist", Name: "x"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Update() on a missing id = %v, want ErrNotFound", err)
	}

	if _, err := repo.Create(ctx, domain.Profile{Name: "dup"}); err != nil {
		t.Fatalf("first Create(): %v", err)
	}
	if _, err := repo.Create(ctx, domain.Profile{Name: "dup"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Create() with a duplicate name = %v, want ErrConflict", err)
	}

	other, err := repo.Create(ctx, domain.Profile{Name: "other"})
	if err != nil {
		t.Fatalf("creating a second profile: %v", err)
	}
	other.Name = "dup"
	if _, err := repo.Update(ctx, other); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Update() renaming a row to collide with a different row's name = %v, want ErrConflict", err)
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

	// UpdatePasswordHash is the only way a password already set can change,
	// so it must keep the row's identity while replacing exactly the hash:
	// a version that reassigned the ID or the username would break every
	// token whose subject_id points at this operator.
	updated, err := repo.UpdatePasswordHash(ctx, "root", []byte("second-hash"))
	if err != nil {
		t.Fatalf("UpdatePasswordHash(): %v", err)
	}
	if updated.ID != created.ID || updated.Username != created.Username {
		t.Fatalf("UpdatePasswordHash() = %+v, want the same id and username as %+v", updated, created)
	}
	if string(updated.PasswordHash) != "second-hash" {
		t.Fatalf("UpdatePasswordHash() hash = %q, want %q", updated.PasswordHash, "second-hash")
	}

	// Read it back through a separate call: the RETURNING clause agreeing
	// with itself would not prove the row on disk actually changed.
	reread, err := repo.ByUsername(ctx, "root")
	if err != nil {
		t.Fatalf("ByUsername() after UpdatePasswordHash(): %v", err)
	}
	if string(reread.PasswordHash) != "second-hash" {
		t.Fatalf("re-read hash = %q, want %q", reread.PasswordHash, "second-hash")
	}

	if n, err := repo.Count(ctx); err != nil || n != 1 {
		t.Fatalf("Count() after UpdatePasswordHash() = %d, %v, want 1, nil — an update must not insert a row", n, err)
	}

	if _, err := repo.UpdatePasswordHash(ctx, "nobody", []byte("hash")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdatePasswordHash() on a missing username = %v, want ErrNotFound", err)
	}
}

// testDeviceProfileMembershipCascadesOnProfileDelete proves device_profiles
// rows are removed when the profile side of the membership is deleted:
// testAliasCascadeDeletesTargeting only ever exercised alias_profiles, so
// this join table's own cascade was previously unverified in either
// direction.
func testDeviceProfileMembershipCascadesOnProfileDelete(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()

	profile, err := st.Profiles().Create(ctx, domain.Profile{Name: "cascade-profile-delete"})
	if err != nil {
		t.Fatalf("creating profile: %v", err)
	}

	if err := st.Tokens().Create(ctx, store.Token{
		Lookup: "cascade-profile-delete-enroll", Kind: store.TokenKindEnrollment, SecretHash: []byte("hash"),
		CreatedAt: time.Now().UTC(), ProfileIDs: []string{profile.ID},
	}); err != nil {
		t.Fatalf("creating enrollment token: %v", err)
	}
	dev, err := st.Tokens().ConsumeEnrollment(ctx, "cascade-profile-delete-enroll", domain.Device{
		Name: "cascade-profile-delete-device", Platform: domain.PlatformLinux, Shell: domain.ShellBash,
	})
	if err != nil {
		t.Fatalf("enrolling device: %v", err)
	}
	if len(dev.ProfileIDs) != 1 || dev.ProfileIDs[0] != profile.ID {
		t.Fatalf("ConsumeEnrollment() = %+v, want ProfileIDs to include %q", dev, profile.ID)
	}

	if err := st.Profiles().Delete(ctx, profile.ID); err != nil {
		t.Fatalf("deleting profile: %v", err)
	}

	got, err := st.Devices().Get(ctx, dev.ID)
	if err != nil {
		t.Fatalf("Get() device after deleting its profile: %v", err)
	}
	if len(got.ProfileIDs) != 0 {
		t.Fatalf("device.ProfileIDs = %v after its profile was deleted, want empty — the join row must cascade away", got.ProfileIDs)
	}
}

// testDeviceProfileMembershipCascadesOnDeviceDelete is
// testDeviceProfileMembershipCascadesOnProfileDelete's mirror for the
// device side of the same join table. It cannot Get() the deleted device
// to inspect membership directly — the device row itself is gone before
// ListDeviceProfileIDs would even run — so it reuses the device's own id
// for a second, freshly enrolled device with no profile membership of its
// own: ListDeviceProfileIDs reads device_profiles by device_id with no
// join back to devices, so if the first device's row had not cascaded
// away, it would leak straight into the second device's membership list
// even though its own enrollment never granted it.
func testDeviceProfileMembershipCascadesOnDeviceDelete(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()

	profile, err := st.Profiles().Create(ctx, domain.Profile{Name: "cascade-device-delete-profile"})
	if err != nil {
		t.Fatalf("creating profile: %v", err)
	}

	const reusedDeviceID = "cascade-device-delete-reused-id"
	if err := st.Tokens().Create(ctx, store.Token{
		Lookup: "cascade-device-delete-enroll-1", Kind: store.TokenKindEnrollment, SecretHash: []byte("hash"),
		CreatedAt: time.Now().UTC(), ProfileIDs: []string{profile.ID},
	}); err != nil {
		t.Fatalf("creating first enrollment token: %v", err)
	}
	dev, err := st.Tokens().ConsumeEnrollment(ctx, "cascade-device-delete-enroll-1", domain.Device{
		ID: reusedDeviceID, Name: "cascade-device-delete-device", Platform: domain.PlatformLinux, Shell: domain.ShellBash,
	})
	if err != nil {
		t.Fatalf("enrolling first device: %v", err)
	}
	if dev.ID != reusedDeviceID {
		t.Fatalf("ConsumeEnrollment() left ID = %q, want the explicit %q preserved", dev.ID, reusedDeviceID)
	}
	if len(dev.ProfileIDs) != 1 {
		t.Fatalf("ConsumeEnrollment() = %+v, want ProfileIDs to include the seeded profile", dev)
	}

	if err := st.Devices().Delete(ctx, dev.ID); err != nil {
		t.Fatalf("deleting device: %v", err)
	}

	if err := st.Tokens().Create(ctx, store.Token{
		Lookup: "cascade-device-delete-enroll-2", Kind: store.TokenKindEnrollment, SecretHash: []byte("hash"),
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("creating second enrollment token: %v", err)
	}
	reenrolled, err := st.Tokens().ConsumeEnrollment(ctx, "cascade-device-delete-enroll-2", domain.Device{
		ID: reusedDeviceID, Name: "cascade-device-delete-device-again", Platform: domain.PlatformLinux, Shell: domain.ShellBash,
	})
	if err != nil {
		t.Fatalf("re-enrolling a device at the reused id: %v", err)
	}
	if len(reenrolled.ProfileIDs) != 0 {
		t.Fatalf("re-enrolled device (reused id %q) has ProfileIDs = %v, want empty — the first device's deleted device_profiles row must not have leaked into it", reusedDeviceID, reenrolled.ProfileIDs)
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

// testTokenLookupConflict is the threat-matrix "token handling" case for
// tokens.lookup's UNIQUE index: TokenRepo.Create routes writes through
// mapWriteError like every other repo, but no test ever created a
// duplicate lookup to prove that path is exercised for this table too.
func testTokenLookupConflict(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	repo := st.Tokens()

	if err := repo.Create(ctx, store.Token{
		Lookup: "dup-lookup", Kind: store.TokenKindSession, SecretHash: []byte("hash"), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("first Create(): %v", err)
	}
	if err := repo.Create(ctx, store.Token{
		Lookup: "dup-lookup", Kind: store.TokenKindDevice, SecretHash: []byte("other-hash"), CreatedAt: time.Now().UTC(),
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Create() with a duplicate lookup = %v, want ErrConflict", err)
	}
}

// testConsumeEnrollmentRejectsExpiredToken is the threat-matrix "token
// handling" case for expiry: no test previously constructed an
// already-expired enrollment token, which is also this project's
// regression fixture for the RFC3339Nano variable-width timestamp bug
// (sqlitestore's expires_at > ? guard compares timestamps as TEXT).
func testConsumeEnrollmentRejectsExpiredToken(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()

	if err := st.Tokens().Create(ctx, store.Token{
		Lookup: "expired-enroll", Kind: store.TokenKindEnrollment, SecretHash: []byte("hash"),
		CreatedAt: time.Now().UTC().Add(-2 * time.Hour),
		ExpiresAt: time.Now().UTC().Add(-1 * time.Hour),
	}); err != nil {
		t.Fatalf("creating an already-expired enrollment token: %v", err)
	}

	if _, err := st.Tokens().ConsumeEnrollment(ctx, "expired-enroll", domain.Device{
		Name: "too-late", Platform: domain.PlatformLinux, Shell: domain.ShellBash,
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("ConsumeEnrollment() on an already-expired token = %v, want ErrConflict", err)
	}

	list, err := st.Devices().List(ctx)
	if err != nil {
		t.Fatalf("Devices().List(): %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("Devices().List() = %d devices after consuming an already-expired token, want 0", len(list))
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

// testAuditAppendAndRecent covers the only two things an audit record has to
// do: survive being written, and come back newest-first. It also pins that
// the actor and subject names are stored as given rather than resolved at
// read time — the point of denormalizing them is that they still answer once
// the operator or the device is gone.
func testAuditAppendAndRecent(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	repo := st.Audit()

	if n, err := repo.Count(ctx); err != nil || n != 0 {
		t.Fatalf("Count() on an empty store = %d, %v, want 0, nil", n, err)
	}

	base := time.Date(2030, time.January, 1, 12, 0, 0, 0, time.UTC)
	for i, e := range []store.AuditEvent{
		{At: base, ActorID: "op-1", ActorName: "admin", Action: store.AuditAliasCreated, SubjectKind: "alias", SubjectID: "alias-1", SubjectLabel: "gs"},
		{At: base.Add(time.Minute), ActorID: "op-1", ActorName: "admin", Action: store.AuditGroupCreated, SubjectKind: "group", SubjectID: "group-1", SubjectLabel: "laptops"},
		{At: base.Add(2 * time.Minute), ActorID: "op-1", ActorName: "admin", Action: store.AuditDeviceRevoked, SubjectKind: "device", SubjectID: "device-1", SubjectLabel: "work-mac"},
	} {
		if err := repo.Append(ctx, e); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	if n, err := repo.Count(ctx); err != nil || n != 3 {
		t.Fatalf("Count() = %d, %v, want 3, nil", n, err)
	}

	recent, err := repo.Recent(ctx, 10)
	if err != nil {
		t.Fatalf("Recent(): %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("Recent(10) returned %d events, want 3", len(recent))
	}
	// Newest first: an audit reader asks "what just happened", not "what
	// happened first".
	if recent[0].Action != store.AuditDeviceRevoked {
		t.Fatalf("first event = %q, want the newest (%q)", recent[0].Action, store.AuditDeviceRevoked)
	}
	if recent[2].Action != store.AuditAliasCreated {
		t.Fatalf("last event = %q, want the oldest (%q)", recent[2].Action, store.AuditAliasCreated)
	}

	got := recent[0]
	if got.ID == "" {
		t.Error("Append() left the ID empty; it must assign one")
	}
	if !got.At.Equal(base.Add(2 * time.Minute)) {
		t.Errorf("At = %v, want the supplied instant %v", got.At, base.Add(2*time.Minute))
	}
	if got.ActorName != "admin" || got.SubjectLabel != "work-mac" {
		t.Errorf("event = %+v, want the actor and subject names stored as given", got)
	}

	// A limit is a limit, and it takes from the newest end.
	limited, err := repo.Recent(ctx, 2)
	if err != nil {
		t.Fatalf("Recent(2): %v", err)
	}
	if len(limited) != 2 || limited[0].Action != store.AuditDeviceRevoked {
		t.Fatalf("Recent(2) = %+v, want the two newest", limited)
	}

	if _, err := repo.Recent(ctx, 0); err == nil {
		t.Error("Recent(0) returned no error; a limit of zero is a caller mistake, not an empty page")
	}
}

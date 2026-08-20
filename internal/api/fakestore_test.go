package api

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/google/uuid"
)

// fakeStore is an in-memory store.Store double used by this package's own
// handler tests. Real backend conformance (cascade behavior, transactional
// atomicity, deterministic ordering, SQL-metacharacter round trips) is
// internal/store/sqlitestore's own storetest suite's job; this fake exists
// only to drive internal/api's HTTP handlers through the shapes and error
// sentinels its own tests need — auth/routing/validation/wiring, not
// storage correctness.
type fakeStore struct {
	mu sync.Mutex

	aliases   map[string]domain.Alias
	profiles  map[string]domain.Profile
	devices   map[string]domain.Device
	operators map[string]store.Operator
	tokens    map[string]store.Token // keyed by Lookup, matching ByLookup's real access pattern

	// byUsernameHook, when non-nil, is invoked by fakeOperatorRepo.ByUsername
	// with the looked-up username immediately after a successful lookup,
	// before returning. It exists only for
	// TestLoginSemaphoreAcquireObservesContextCancellationAfterUsernameLookup
	// (auth_test.go) to signal a test goroutine at the exact instant
	// handleLogin is about to reach its limiter acquire, so that test can
	// cancel the calling request's own context from precisely that window —
	// after ByUsername succeeded, before the acquire — and nowhere else.
	byUsernameHook func(username string)

	// tokenCreateErr, when non-nil, is returned by exactly the next
	// fakeTokenRepo.Create call and then cleared. It exists only for
	// devices_test.go's two WARNING-4 tests
	// (TestDevicesRegisterLeavesADiscoverableDeviceWhenTokenIssuanceFails,
	// TestDevicesRotateTokenIsSafeToRetryWhenTokenIssuanceFails) to force the
	// exact "second, separate write in a two-step operation fails" window
	// without touching internal/store.
	tokenCreateErr error

	// touchErr, when non-nil, is returned by exactly the next
	// fakeDeviceRepo.Touch call and then cleared. It exists only for
	// sync_test.go's TestSyncServesResolvedAliasesWhenTouchFails
	// (bounded-review finding 1) to force handleSync's own bookkeeping
	// write to fail after sync.Resolve has already succeeded, without
	// touching internal/store.
	touchErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		aliases:   map[string]domain.Alias{},
		profiles:  map[string]domain.Profile{},
		devices:   map[string]domain.Device{},
		operators: map[string]store.Operator{},
		tokens:    map[string]store.Token{},
	}
}

func (s *fakeStore) Aliases() store.AliasRepo      { return fakeAliasRepo{s} }
func (s *fakeStore) Devices() store.DeviceRepo     { return fakeDeviceRepo{s} }
func (s *fakeStore) Profiles() store.ProfileRepo   { return fakeProfileRepo{s} }
func (s *fakeStore) Tokens() store.TokenRepo       { return fakeTokenRepo{s} }
func (s *fakeStore) Operators() store.OperatorRepo { return fakeOperatorRepo{s} }
func (s *fakeStore) Close() error                  { return nil }

// --- aliases ---

type fakeAliasRepo struct{ s *fakeStore }

func (r fakeAliasRepo) Create(_ context.Context, a domain.Alias) (domain.Alias, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.checkAliasReferencesLocked(a); err != nil {
		return domain.Alias{}, err
	}
	for _, existing := range s.aliases {
		if existing.Name == a.Name {
			return domain.Alias{}, store.ErrConflict
		}
	}

	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	now := time.Now()
	a.CreatedAt, a.UpdatedAt = now, now
	s.aliases[a.ID] = a
	return a, nil
}

func (r fakeAliasRepo) Get(_ context.Context, id string) (domain.Alias, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.aliases[id]
	if !ok {
		return domain.Alias{}, store.ErrNotFound
	}
	return a, nil
}

func (r fakeAliasRepo) List(_ context.Context) ([]domain.Alias, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Alias, 0, len(s.aliases))
	for _, a := range s.aliases {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r fakeAliasRepo) Update(_ context.Context, a domain.Alias) (domain.Alias, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.aliases[a.ID]
	if !ok {
		return domain.Alias{}, store.ErrNotFound
	}
	for id, other := range s.aliases {
		if id != a.ID && other.Name == a.Name {
			return domain.Alias{}, store.ErrConflict
		}
	}
	if err := s.checkAliasReferencesLocked(a); err != nil {
		return domain.Alias{}, err
	}

	a.CreatedAt = existing.CreatedAt
	a.UpdatedAt = time.Now()
	s.aliases[a.ID] = a
	return a, nil
}

func (r fakeAliasRepo) Delete(_ context.Context, id string) error {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.aliases[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.aliases, id)
	return nil
}

// checkAliasReferencesLocked reports store.ErrInvalidReference if a names a
// profile or device that does not exist. Callers must already hold s.mu.
func (s *fakeStore) checkAliasReferencesLocked(a domain.Alias) error {
	for _, id := range a.ProfileIDs {
		if _, ok := s.profiles[id]; !ok {
			return store.ErrInvalidReference
		}
	}
	for _, id := range a.DeviceIDs {
		if _, ok := s.devices[id]; !ok {
			return store.ErrInvalidReference
		}
	}
	return nil
}

// --- profiles ---

type fakeProfileRepo struct{ s *fakeStore }

func (r fakeProfileRepo) Create(_ context.Context, p domain.Profile) (domain.Profile, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.profiles {
		if existing.Name == p.Name {
			return domain.Profile{}, store.ErrConflict
		}
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	s.profiles[p.ID] = p
	return p, nil
}

func (r fakeProfileRepo) Get(_ context.Context, id string) (domain.Profile, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[id]
	if !ok {
		return domain.Profile{}, store.ErrNotFound
	}
	return p, nil
}

func (r fakeProfileRepo) List(_ context.Context) ([]domain.Profile, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Profile, 0, len(s.profiles))
	for _, p := range s.profiles {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r fakeProfileRepo) Update(_ context.Context, p domain.Profile) (domain.Profile, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.profiles[p.ID]
	if !ok {
		return domain.Profile{}, store.ErrNotFound
	}
	for id, other := range s.profiles {
		if id != p.ID && other.Name == p.Name {
			return domain.Profile{}, store.ErrConflict
		}
	}
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = time.Now()
	s.profiles[p.ID] = p
	return p, nil
}

func (r fakeProfileRepo) Delete(_ context.Context, id string) error {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.profiles[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.profiles, id)
	return nil
}

// --- devices ---

type fakeDeviceRepo struct{ s *fakeStore }

func (r fakeDeviceRepo) Get(_ context.Context, id string) (domain.Device, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[id]
	if !ok {
		return domain.Device{}, store.ErrNotFound
	}
	return d, nil
}

func (r fakeDeviceRepo) List(_ context.Context) ([]domain.Device, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Device, 0, len(s.devices))
	for _, d := range s.devices {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r fakeDeviceRepo) Update(_ context.Context, d domain.Device) (domain.Device, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.devices[d.ID]
	if !ok {
		return domain.Device{}, store.ErrNotFound
	}
	for _, id := range d.ProfileIDs {
		if _, ok := s.profiles[id]; !ok {
			return domain.Device{}, store.ErrInvalidReference
		}
	}
	existing.Name = d.Name
	existing.ProfileIDs = d.ProfileIDs
	s.devices[d.ID] = existing
	return existing, nil
}

func (r fakeDeviceRepo) Delete(_ context.Context, id string) error {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.devices[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.devices, id)
	return nil
}

// Touch mirrors sqlitestore's own behavior (design decision 10) closely
// enough to matter: it must actually persist at into LastSeenAt/LastSyncAt,
// not merely accept the parameter and drop it. A fake that ignores at while
// still returning nil is exactly the "fake more permissive than the real
// store" failure shape this project has hit before — any test asserting on
// these two fields would pass against this fake yet tell nothing about
// production behavior if this method silently no-op'd them.
func (r fakeDeviceRepo) Touch(_ context.Context, id string, platform domain.Platform, sh domain.Shell, at time.Time) error {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.touchErr != nil {
		err := s.touchErr
		s.touchErr = nil
		return err
	}
	d, ok := s.devices[id]
	if !ok {
		return store.ErrNotFound
	}
	d.Platform, d.Shell = platform, sh
	seenAt, syncAt := at, at
	d.LastSeenAt = &seenAt
	d.LastSyncAt = &syncAt
	s.devices[id] = d
	return nil
}

func (r fakeDeviceRepo) Heartbeat(_ context.Context, id string, at time.Time) error {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[id]
	if !ok {
		return store.ErrNotFound
	}
	seenAt := at
	d.LastSeenAt = &seenAt
	s.devices[id] = d
	return nil
}

func (r fakeDeviceRepo) Revoke(_ context.Context, id string, _ time.Time) error {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.devices[id]; !ok {
		return store.ErrNotFound
	}
	return nil
}

// --- operators ---

type fakeOperatorRepo struct{ s *fakeStore }

func (r fakeOperatorRepo) Create(_ context.Context, o store.Operator) (store.Operator, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.operators {
		if existing.Username == o.Username {
			return store.Operator{}, store.ErrConflict
		}
	}
	if o.ID == "" {
		o.ID = uuid.NewString()
	}
	now := time.Now()
	o.CreatedAt, o.UpdatedAt = now, now
	s.operators[o.ID] = o
	return o, nil
}

func (r fakeOperatorRepo) Get(_ context.Context, id string) (store.Operator, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.operators[id]
	if !ok {
		return store.Operator{}, store.ErrNotFound
	}
	return o, nil
}

func (r fakeOperatorRepo) ByUsername(_ context.Context, username string) (store.Operator, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.operators {
		if o.Username == username {
			if s.byUsernameHook != nil {
				s.byUsernameHook(username)
			}
			return o, nil
		}
	}
	return store.Operator{}, store.ErrNotFound
}

func (r fakeOperatorRepo) Count(_ context.Context) (int, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.operators), nil
}

func (r fakeOperatorRepo) UpdatePasswordHash(_ context.Context, username string, hash []byte) (store.Operator, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, o := range s.operators {
		if o.Username == username {
			o.PasswordHash = hash
			o.UpdatedAt = time.Now()
			s.operators[id] = o
			return o, nil
		}
	}
	return store.Operator{}, store.ErrNotFound
}

// --- tokens ---

type fakeTokenRepo struct{ s *fakeStore }

func (r fakeTokenRepo) Create(_ context.Context, t store.Token) error {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokenCreateErr != nil {
		err := s.tokenCreateErr
		s.tokenCreateErr = nil
		return err
	}
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	s.tokens[t.Lookup] = t
	return nil
}

func (r fakeTokenRepo) ByLookup(_ context.Context, lookup string) (store.Token, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[lookup]
	if !ok {
		return store.Token{}, store.ErrNotFound
	}
	return t, nil
}

// ConsumeEnrollment mirrors the real backend's guard shape closely enough
// for this package's own handler tests (kind check, single-use, expiry,
// dangling ProfileIDs reference) without claiming to prove the real
// backend's transactional atomicity — that property belongs to
// sqlitestore's own concurrency-specific tests
// (ConsumeEnrollmentTokenGuardIsAtomicUnderRealConcurrency and its forced-
// interleaving sibling), which this fake cannot and does not attempt to
// substitute for.
func (r fakeTokenRepo) ConsumeEnrollment(_ context.Context, lookup string, dev domain.Device) (domain.Device, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()

	tok, ok := s.tokens[lookup]
	if !ok {
		return domain.Device{}, store.ErrNotFound
	}
	if tok.Kind != store.TokenKindEnrollment {
		return domain.Device{}, store.ErrConflict
	}
	if !tok.UsedAt.IsZero() || !tok.RevokedAt.IsZero() {
		return domain.Device{}, store.ErrConflict
	}
	if !tok.ExpiresAt.IsZero() && !time.Now().Before(tok.ExpiresAt) {
		return domain.Device{}, store.ErrConflict
	}
	for _, id := range tok.ProfileIDs {
		if _, ok := s.profiles[id]; !ok {
			return domain.Device{}, store.ErrInvalidReference
		}
	}

	if dev.ID == "" {
		dev.ID = uuid.NewString()
	}
	dev.ProfileIDs = tok.ProfileIDs
	s.devices[dev.ID] = dev

	tok.UsedAt = time.Now()
	tok.SubjectID = dev.ID
	s.tokens[lookup] = tok

	return dev, nil
}

func (r fakeTokenRepo) Revoke(_ context.Context, id string, at time.Time) error {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	for lookup, t := range s.tokens {
		if t.ID == id {
			t.RevokedAt = at
			s.tokens[lookup] = t
			return nil
		}
	}
	return store.ErrNotFound
}

func (r fakeTokenRepo) RevokeSubject(_ context.Context, kind store.TokenKind, subjectID string, at time.Time) error {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	for lookup, t := range s.tokens {
		if t.Kind == kind && t.SubjectID == subjectID && t.RevokedAt.IsZero() {
			t.RevokedAt = at
			s.tokens[lookup] = t
		}
	}
	return nil
}

// --- test helpers shared across aliases_test.go, profiles_test.go,
// devices_test.go and auth_test.go ---

// newFakeStoreWithOperator seeds s with one operator at username/password
// (hashed via auth.HashPassword — the same function production login
// verifies against), and returns the operator's id alongside s so a test
// can mint tokens for it directly.
func newFakeStoreWithOperator(username, password string) (*fakeStore, string) {
	s := newFakeStore()
	hash, err := auth.HashPassword(password)
	if err != nil {
		panic(fmt.Sprintf("test setup: hashing password: %v", err))
	}
	op, err := s.Operators().Create(context.Background(), store.Operator{
		Username:     username,
		PasswordHash: []byte(hash),
	})
	if err != nil {
		panic(fmt.Sprintf("test setup: creating operator: %v", err))
	}
	return s, op.ID
}

// mintSessionFor mints a real session token for subjectID, persists it in
// s, and returns the wire value a test attaches as
// "Authorization: Bearer <wire>". Using auth.Mint (production code) rather
// than a synthetic string is what makes a test exercising this token also
// exercise RequireKind's real parse/lookup/verify path.
func mintSessionFor(s *fakeStore, subjectID string) string {
	minted, err := auth.Mint(store.TokenKindSession)
	if err != nil {
		panic(fmt.Sprintf("test setup: minting session token: %v", err))
	}
	if err := s.Tokens().Create(context.Background(), store.Token{
		Kind:       store.TokenKindSession,
		SubjectID:  subjectID,
		Lookup:     minted.Lookup,
		SecretHash: minted.SecretHash,
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(sessionLifetime),
	}); err != nil {
		panic(fmt.Sprintf("test setup: persisting session token: %v", err))
	}
	return minted.Wire
}

// mintEnrollmentToken mints and persists a real enrollment token, returning
// its wire value, for tests that need to exercise
// devicesRegisterPattern/auth.ConsumeEnrollment end to end.
func mintEnrollmentToken(s *fakeStore, profileIDs []string, expiresAt time.Time) string {
	minted, err := auth.Mint(store.TokenKindEnrollment)
	if err != nil {
		panic(fmt.Sprintf("test setup: minting enrollment token: %v", err))
	}
	if err := s.Tokens().Create(context.Background(), store.Token{
		Kind:       store.TokenKindEnrollment,
		SecretHash: minted.SecretHash,
		Lookup:     minted.Lookup,
		ProfileIDs: profileIDs,
		CreatedAt:  time.Now(),
		ExpiresAt:  expiresAt,
	}); err != nil {
		panic(fmt.Sprintf("test setup: persisting enrollment token: %v", err))
	}
	return minted.Wire
}

// mintDeviceTokenFor mints and persists a real device-kind token for
// deviceID directly, for sync_test.go's cases that need a device already
// authenticated without re-running the full registration exchange (whose
// own coverage is devices_test.go's job). Using auth.Mint (production code)
// keeps this exercising RequireKind's real parse/lookup/verify path, exactly
// like mintSessionFor does for operator sessions.
func mintDeviceTokenFor(s *fakeStore, deviceID string) string {
	minted, err := auth.Mint(store.TokenKindDevice)
	if err != nil {
		panic(fmt.Sprintf("test setup: minting device token: %v", err))
	}
	if err := s.Tokens().Create(context.Background(), store.Token{
		Kind:       store.TokenKindDevice,
		SubjectID:  deviceID,
		Lookup:     minted.Lookup,
		SecretHash: minted.SecretHash,
		CreatedAt:  time.Now(),
	}); err != nil {
		panic(fmt.Sprintf("test setup: persisting device token: %v", err))
	}
	return minted.Wire
}

package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

func TestMintProducesAParsableWireToken(t *testing.T) {
	for _, kind := range []store.TokenKind{store.TokenKindSession, store.TokenKindEnrollment, store.TokenKindDevice} {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			minted, err := Mint(kind)
			if err != nil {
				t.Fatalf("Mint(%q) = %v, want nil error", kind, err)
			}
			if minted.Wire == "" {
				t.Fatal("Mint() left Wire empty")
			}
			if minted.Lookup == "" {
				t.Fatal("Mint() left Lookup empty")
			}
			if len(minted.SecretHash) != 32 {
				t.Fatalf("Mint() SecretHash is %d bytes, want 32 (sha256)", len(minted.SecretHash))
			}

			parsed, err := Parse(minted.Wire)
			if err != nil {
				t.Fatalf("Parse(%q) = %v, want nil error", minted.Wire, err)
			}
			if parsed.Kind != kind {
				t.Fatalf("Parse() kind = %q, want %q", parsed.Kind, kind)
			}
			if parsed.Lookup != minted.Lookup {
				t.Fatalf("Parse() lookup = %q, want %q", parsed.Lookup, minted.Lookup)
			}
			if parsed.Secret == "" {
				t.Fatal("Parse() left Secret empty")
			}

			if !VerifySecret(parsed.Secret, minted.SecretHash) {
				t.Fatal("VerifySecret() rejected the secret Mint() itself produced")
			}
		})
	}
}

func TestMintTokensAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		minted, err := Mint(store.TokenKindDevice)
		if err != nil {
			t.Fatalf("Mint(): %v", err)
		}
		if seen[minted.Wire] {
			t.Fatalf("Mint() produced a duplicate wire token: %q", minted.Wire)
		}
		seen[minted.Wire] = true
	}
}

func TestParseRefusesMalformedTokensWithoutPanicking(t *testing.T) {
	tests := []struct {
		name string
		wire string
	}{
		{"empty string", ""},
		{"wrong prefix", "xd1_YWJjZGVm.c2VjcmV0"},
		{"unknown kind byte", "adx_YWJjZGVm.c2VjcmV0"},
		{"missing underscore", "adsYWJjZGVm.c2VjcmV0"},
		{"missing separator dot", "ads_YWJjZGVmc2VjcmV0"},
		{"empty lookup", "ads_.c2VjcmV0"},
		{"empty secret", "ads_YWJjZGVm."},
		{"just the prefix", "ad"},
		{"kind byte with nothing after", "ads"},
		{"only the kind and underscore", "ads_"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse(%q) panicked: %v", tt.wire, r)
				}
			}()

			_, err := Parse(tt.wire)
			if !errors.Is(err, ErrMalformedToken) {
				t.Fatalf("Parse(%q) = %v, want ErrMalformedToken", tt.wire, err)
			}
		})
	}
}

func TestVerifySecretRejectsWrongSecret(t *testing.T) {
	minted, err := Mint(store.TokenKindSession)
	if err != nil {
		t.Fatalf("Mint(): %v", err)
	}

	if VerifySecret("not-the-real-secret", minted.SecretHash) {
		t.Fatal("VerifySecret() accepted a wrong secret")
	}
}

func TestVerifyRejectsWrongKindForTheRoute(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok := store.Token{Kind: store.TokenKindDevice}

	if err := Verify(tok, store.TokenKindSession, now); !errors.Is(err, ErrWrongTokenKind) {
		t.Fatalf("Verify() accepted a device token where an operator session was required = %v, want ErrWrongTokenKind", err)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tok := store.Token{
		Kind:      store.TokenKindSession,
		ExpiresAt: now.Add(-1 * time.Second),
	}

	if err := Verify(tok, store.TokenKindSession, now); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("Verify() on an expired token = %v, want ErrExpiredToken", err)
	}
}

func TestVerifyAcceptsATokenWithNoExpiry(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tok := store.Token{Kind: store.TokenKindDevice} // zero ExpiresAt means never

	if err := Verify(tok, store.TokenKindDevice, now); err != nil {
		t.Fatalf("Verify() on a never-expiring token = %v, want nil", err)
	}
}

func TestVerifyAcceptsATokenOneSecondBeforeItsExpiry(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tok := store.Token{
		Kind:      store.TokenKindSession,
		ExpiresAt: now.Add(1 * time.Second),
	}

	if err := Verify(tok, store.TokenKindSession, now); err != nil {
		t.Fatalf("Verify() one second before expiry = %v, want nil", err)
	}
}

func TestVerifyRejectsRevokedToken(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok := store.Token{
		Kind:      store.TokenKindDevice,
		RevokedAt: now.Add(-1 * time.Minute),
	}

	if err := Verify(tok, store.TokenKindDevice, now); !errors.Is(err, ErrRevokedToken) {
		t.Fatalf("Verify() on a revoked token = %v, want ErrRevokedToken", err)
	}
}

// fakeTokenRepo is an in-memory store.TokenRepo used by auth's own tests so
// they stay pure unit tests (no SQLite, no t.TempDir) per the testing
// strategy's "N/A — pure unit tests, injected now func()" row. It mimics the
// one guarantee ConsumeEnrollment's callers depend on: a token already
// marked used refuses a second consumption instead of minting a second
// device.
type fakeTokenRepo struct {
	tokens  map[string]store.Token
	devices []domain.Device
}

func newFakeTokenRepo() *fakeTokenRepo {
	return &fakeTokenRepo{tokens: map[string]store.Token{}}
}

func (f *fakeTokenRepo) Create(_ context.Context, t store.Token) error {
	if _, exists := f.tokens[t.Lookup]; exists {
		return store.ErrConflict
	}
	if t.ID == "" {
		t.ID = t.Lookup
	}
	f.tokens[t.Lookup] = t
	return nil
}

func (f *fakeTokenRepo) ByLookup(_ context.Context, lookup string) (store.Token, error) {
	tok, ok := f.tokens[lookup]
	if !ok {
		return store.Token{}, store.ErrNotFound
	}
	return tok, nil
}

func (f *fakeTokenRepo) ConsumeEnrollment(_ context.Context, lookup string, dev domain.Device) (domain.Device, error) {
	tok, ok := f.tokens[lookup]
	if !ok {
		return domain.Device{}, store.ErrNotFound
	}
	if !tok.UsedAt.IsZero() || !tok.RevokedAt.IsZero() {
		return domain.Device{}, store.ErrConflict
	}
	if !tok.ExpiresAt.IsZero() && !time.Now().UTC().Before(tok.ExpiresAt) {
		return domain.Device{}, store.ErrConflict
	}
	tok.UsedAt = time.Now().UTC()
	f.tokens[lookup] = tok
	if dev.ID == "" {
		dev.ID = "device-" + lookup
	}
	f.devices = append(f.devices, dev)
	return dev, nil
}

func (f *fakeTokenRepo) Revoke(_ context.Context, id string, at time.Time) error {
	for lookup, tok := range f.tokens {
		if tok.ID == id {
			tok.RevokedAt = at
			f.tokens[lookup] = tok
			return nil
		}
	}
	return store.ErrNotFound
}

func (f *fakeTokenRepo) RevokeSubject(_ context.Context, kind store.TokenKind, subjectID string, at time.Time) error {
	for lookup, tok := range f.tokens {
		if tok.Kind == kind && tok.SubjectID == subjectID {
			tok.RevokedAt = at
			f.tokens[lookup] = tok
		}
	}
	return nil
}

func TestConsumeEnrollmentAuthenticatesBeforeDelegating(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTokenRepo()

	minted, err := Mint(store.TokenKindEnrollment)
	if err != nil {
		t.Fatalf("Mint(): %v", err)
	}
	if err := repo.Create(ctx, store.Token{
		Lookup: minted.Lookup, Kind: store.TokenKindEnrollment, SecretHash: minted.SecretHash,
	}); err != nil {
		t.Fatalf("seeding token: %v", err)
	}

	if _, err := ConsumeEnrollment(ctx, repo, "adx_wrong.wire", domain.Device{Name: "d"}); !errors.Is(err, ErrMalformedToken) {
		t.Fatalf("ConsumeEnrollment() with a malformed wire token = %v, want ErrMalformedToken", err)
	}

	wrongSecret := "ade_" + minted.Lookup + ".not-the-secret"
	if _, err := ConsumeEnrollment(ctx, repo, wrongSecret, domain.Device{Name: "d"}); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("ConsumeEnrollment() with a wrong secret = %v, want ErrWrongSecret", err)
	}
	if len(repo.devices) != 0 {
		t.Fatalf("ConsumeEnrollment() with a wrong secret created %d devices, want 0", len(repo.devices))
	}

	dev, err := ConsumeEnrollment(ctx, repo, minted.Wire, domain.Device{Name: "laptop"})
	if err != nil {
		t.Fatalf("ConsumeEnrollment() with the correct token = %v, want nil error", err)
	}
	if dev.ID == "" {
		t.Fatal("ConsumeEnrollment() left the device ID empty")
	}
}

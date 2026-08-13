package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// TestReplayedEnrollmentTokenIsRefusedEndToEnd is the threat-matrix "token
// handling" scenario, and the server-auth spec's "Reused enrollment token
// is refused" scenario: a second register attempt presenting an
// already-consumed enrollment token must be refused, and must not mint a
// second device. It exercises the full authenticate-then-consume sequence
// (auth.ConsumeEnrollment) end to end against a token that was already
// used once, the same way a second `register` invocation would replay a
// captured or reused enrollment token wire value.
func TestReplayedEnrollmentTokenIsRefusedEndToEnd(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTokenRepo()

	minted, err := Mint(store.TokenKindEnrollment)
	if err != nil {
		t.Fatalf("Mint(): %v", err)
	}
	if err := repo.Create(ctx, store.Token{
		Lookup: minted.Lookup, Kind: store.TokenKindEnrollment, SecretHash: minted.SecretHash,
	}); err != nil {
		t.Fatalf("seeding enrollment token: %v", err)
	}

	first, err := ConsumeEnrollment(ctx, repo, minted.Wire, domain.Device{Name: "first-registration"})
	if err != nil {
		t.Fatalf("first ConsumeEnrollment() (the legitimate registration) = %v, want nil error", err)
	}
	if first.ID == "" {
		t.Fatal("first ConsumeEnrollment() left the device ID empty")
	}

	// Replay: the exact same wire token, presented again — a captured or
	// resubmitted registration request.
	_, err = ConsumeEnrollment(ctx, repo, minted.Wire, domain.Device{Name: "replayed-registration"})
	if err == nil {
		t.Fatal("second ConsumeEnrollment() with an already-consumed token returned nil error, want it refused")
	}
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second ConsumeEnrollment() with an already-consumed token = %v, want store.ErrConflict", err)
	}

	if len(repo.devices) != 1 {
		t.Fatalf("devices created after a replayed enrollment token = %d, want exactly 1 (only the first, legitimate registration)", len(repo.devices))
	}
	if repo.devices[0].Name != "first-registration" {
		t.Fatalf("the surviving device is %q, want it to be the first legitimate registration, not the replay", repo.devices[0].Name)
	}
}

// TestConsumeEnrollmentRefusesNonEnrollmentKindTokens is the regression
// test for a bounded-review finding: ConsumeEnrollment's own kind guard
// (`parsed.Kind != store.TokenKindEnrollment`) was never exercised by any
// existing test. The test that looked like it covered this,
// "adx_wrong.wire" in TestConsumeEnrollmentAuthenticatesBeforeDelegating,
// fails inside Parse itself — 'x' is not a known kind byte — so it never
// reaches the guard. This presents a real, well-formed device-kind and
// session-kind wire token (the shape a stolen device token or an
// operator's own session token actually has) and requires the guard to
// refuse both before ConsumeEnrollment ever calls tokens.ByLookup.
func TestConsumeEnrollmentRefusesNonEnrollmentKindTokens(t *testing.T) {
	ctx := context.Background()

	for _, kind := range []store.TokenKind{store.TokenKindDevice, store.TokenKindSession} {
		t.Run(string(kind), func(t *testing.T) {
			repo := newFakeTokenRepo()

			minted, err := Mint(kind)
			if err != nil {
				t.Fatalf("Mint(%q): %v", kind, err)
			}
			if err := repo.Create(ctx, store.Token{
				Lookup: minted.Lookup, Kind: kind, SecretHash: minted.SecretHash,
			}); err != nil {
				t.Fatalf("seeding %s token: %v", kind, err)
			}

			_, err = ConsumeEnrollment(ctx, repo, minted.Wire, domain.Device{Name: "should-not-register"})
			if !errors.Is(err, ErrWrongTokenKind) {
				t.Fatalf("ConsumeEnrollment() with a %s-kind wire token = %v, want ErrWrongTokenKind", kind, err)
			}
			if len(repo.devices) != 0 {
				t.Fatalf("ConsumeEnrollment() with a %s-kind wire token created %d devices, want 0", kind, len(repo.devices))
			}
		})
	}
}

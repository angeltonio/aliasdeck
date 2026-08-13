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

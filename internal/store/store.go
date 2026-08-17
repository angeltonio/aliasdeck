package store

import (
	"context"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// Store is the server's persistence seam.
//
// No method on Store or on any repository it returns names a driver type or
// a SQLite dialect string (design decision 3): every parameter and return
// type is either a stdlib type, a domain type, or one declared in this
// package. internal/store/sqlitestore is the only implementation shipped in
// this milestone; a future PostgreSQL backend would satisfy this same seam
// without changing a single call site.
type Store interface {
	Aliases() AliasRepo
	Devices() DeviceRepo
	Profiles() ProfileRepo
	Tokens() TokenRepo
	Operators() OperatorRepo
	Close() error
}

// AliasRepo persists domain.Alias, including its targeting (Platforms,
// Shells, ProfileIDs, DeviceIDs).
//
// List returns the full set, enabled and disabled alike, with targeting
// intact and no device filtering applied: design decision 4 keeps
// resolution logic in internal/sync (which calls domain.Resolve), never in
// a WHERE clause here.
type AliasRepo interface {
	// Create persists a, assigning an ID if a.ID is empty. It returns
	// ErrConflict if a.Name is already taken, or ErrInvalidReference if
	// a.ProfileIDs or a.DeviceIDs names a profile or device that does not
	// exist.
	Create(ctx context.Context, a domain.Alias) (domain.Alias, error)

	// Get returns the alias with the given ID, or ErrNotFound.
	Get(ctx context.Context, id string) (domain.Alias, error)

	// List returns every alias, ordered deterministically by name.
	List(ctx context.Context) ([]domain.Alias, error)

	// Update replaces the alias with a.ID's fields and targeting. It
	// returns ErrNotFound if no alias with that ID exists, ErrConflict if
	// the new name collides with a different alias, or ErrInvalidReference
	// if a.ProfileIDs or a.DeviceIDs names a profile or device that does
	// not exist.
	Update(ctx context.Context, a domain.Alias) (domain.Alias, error)

	// Delete removes the alias with the given ID and its targeting join
	// rows. It returns ErrNotFound if no alias with that ID exists.
	Delete(ctx context.Context, id string) error
}

// BoundedAliasCreator is the production store extension used by HTTP create
// paths to make the capacity check and insertion one atomic store operation.
type BoundedAliasCreator interface {
	CreateWithinLimit(ctx context.Context, a domain.Alias, limit int) (domain.Alias, error)
}

// DeviceRepo persists domain.Device.
//
// There is no Create: a device is born only through
// TokenRepo.ConsumeEnrollment, which is what makes enrollment atomic
// (design's Interfaces section).
type DeviceRepo interface {
	// Get returns the device with the given ID, or ErrNotFound.
	Get(ctx context.Context, id string) (domain.Device, error)

	// List returns every device, ordered deterministically by name.
	List(ctx context.Context) ([]domain.Device, error)

	// Update replaces the device's name and profile membership. It
	// returns ErrNotFound if no device with that ID exists, or
	// ErrInvalidReference if d.ProfileIDs names a profile that does not
	// exist.
	Update(ctx context.Context, d domain.Device) (domain.Device, error)

	// Delete removes the device and its profile/alias-targeting join
	// rows. It returns ErrNotFound if no device with that ID exists.
	Delete(ctx context.Context, id string) error

	// Touch records the platform and shell the device reported and
	// stamps last_seen_at/last_sync_at with at. This is the write a sync
	// GET performs on the device row (design decision 10). It returns
	// ErrNotFound if no device with that ID exists.
	Touch(ctx context.Context, id string, platform domain.Platform, shell domain.Shell, at time.Time) error

	// Heartbeat records that a device reached the control plane without
	// changing its last successful alias synchronization. It returns
	// ErrNotFound if no device with that ID exists.
	Heartbeat(ctx context.Context, id string, at time.Time) error

	// Revoke marks the device revoked at the given time, invalidating any
	// device token minted for it. It returns ErrNotFound if no device
	// with that ID exists.
	Revoke(ctx context.Context, id string, at time.Time) error
}

// ProfileRepo persists domain.Profile.
type ProfileRepo interface {
	// Create persists p, assigning an ID if p.ID is empty. It returns
	// ErrConflict if p.Name is already taken.
	Create(ctx context.Context, p domain.Profile) (domain.Profile, error)

	// Get returns the profile with the given ID, or ErrNotFound.
	Get(ctx context.Context, id string) (domain.Profile, error)

	// List returns every profile, ordered deterministically by name.
	List(ctx context.Context) ([]domain.Profile, error)

	// Update replaces the profile's fields. It returns ErrNotFound if no
	// profile with that ID exists, or ErrConflict if the new name
	// collides with a different profile.
	Update(ctx context.Context, p domain.Profile) (domain.Profile, error)

	// Delete removes the profile and its membership join rows. It
	// returns ErrNotFound if no profile with that ID exists.
	Delete(ctx context.Context, id string) error
}

// Operator is a person who can administer the server: create aliases,
// profiles, devices and tokens. It is a store-level type, not a domain
// type, because operators only exist in control-plane mode.
type Operator struct {
	ID           string
	Username     string
	PasswordHash []byte
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// OperatorRepo persists Operator.
type OperatorRepo interface {
	// Create persists o, assigning an ID if o.ID is empty. It returns
	// ErrConflict if o.Username is already taken.
	Create(ctx context.Context, o Operator) (Operator, error)

	// Get returns the operator with the given ID, or ErrNotFound.
	Get(ctx context.Context, id string) (Operator, error)

	// ByUsername returns the operator with the given username, or
	// ErrNotFound. This is the lookup auth performs on login.
	ByUsername(ctx context.Context, username string) (Operator, error)

	// Count reports how many operators exist. Bootstrap (internal/auth)
	// uses this to decide whether the server is starting against an
	// empty database.
	Count(ctx context.Context) (int, error)
}

// TokenKind distinguishes the three purposes a token can serve. Wire form
// and lifetimes are design decision 8's table; this package only persists
// the row.
type TokenKind string

const (
	TokenKindSession    TokenKind = "session"
	TokenKindEnrollment TokenKind = "enrollment"
	TokenKindDevice     TokenKind = "device"
)

// Token is one row in the tokens table. Lookup is unique-indexed plain
// text; SecretHash is the sha256 of the secret half of the wire token
// (design decision 8) — the plaintext secret itself is never persisted.
type Token struct {
	ID         string
	Lookup     string
	Kind       TokenKind
	SubjectID  string // operator id, device id, or "" for an unconsumed enrollment
	SecretHash []byte
	ProfileIDs []string // enrollment only: what the registered device joins
	CreatedAt  time.Time
	ExpiresAt  time.Time // zero = never
	UsedAt     time.Time // enrollment only
	RevokedAt  time.Time
}

// TokenRepo persists Token and performs the one operation that must be
// atomic: consuming an enrollment token to mint a device.
type TokenRepo interface {
	// Create persists t, assigning an ID if t.ID is empty.
	Create(ctx context.Context, t Token) error

	// ByLookup returns the token with the given lookup value, or
	// ErrNotFound. This is the one-row lookup authentication performs on
	// every request (design decision 8).
	ByLookup(ctx context.Context, lookup string) (Token, error)

	// ConsumeEnrollment atomically marks the enrollment token at lookup
	// used and creates dev. The UPDATE ... WHERE used_at IS NULL guard
	// and the device INSERT share one transaction, so two concurrent
	// calls with the same lookup yield exactly one device: the loser
	// observes zero rows affected and returns an error, never a second
	// device. It returns ErrNotFound if the token does not exist,
	// ErrConflict if it was already used, revoked, or has expired, or
	// ErrInvalidReference if the token's ProfileIDs names a profile that
	// does not exist.
	ConsumeEnrollment(ctx context.Context, lookup string, dev domain.Device) (domain.Device, error)

	// Revoke marks the token with the given ID revoked at the given
	// time. It returns ErrNotFound if no token with that ID exists.
	Revoke(ctx context.Context, id string, at time.Time) error

	// RevokeSubject marks every non-revoked token of kind belonging to
	// subjectID revoked at the given time — "log out everywhere" for a
	// session, or invalidating every device token for a device.
	RevokeSubject(ctx context.Context, kind TokenKind, subjectID string, at time.Time) error
}

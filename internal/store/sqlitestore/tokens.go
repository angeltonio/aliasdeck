package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/google/uuid"
)

// tokenRepo implements store.TokenRepo. It keeps its own *sql.DB reference
// because ConsumeEnrollment's guarantee — the UPDATE ... WHERE used_at IS
// NULL and the device INSERT share one transaction — is the one thing in
// this package that MUST be atomic (design's Interfaces section).
type tokenRepo struct {
	db *sql.DB
	q  *Queries
}

func (r tokenRepo) Create(ctx context.Context, t store.Token) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	profileIDs, err := encodeStrings(t.ProfileIDs)
	if err != nil {
		return err
	}

	createdAt := t.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	err = r.q.CreateToken(ctx, CreateTokenParams{
		ID: t.ID, Kind: string(t.Kind), SubjectID: t.SubjectID, Lookup: t.Lookup,
		SecretHash: t.SecretHash, ProfileIds: profileIDs,
		CreatedAt: formatTime(createdAt),
		ExpiresAt: formatNullableTime(t.ExpiresAt),
		UsedAt:    formatNullableTime(t.UsedAt),
		RevokedAt: formatNullableTime(t.RevokedAt),
	})
	if err != nil {
		return mapWriteError("creating token", err)
	}
	return nil
}

func (r tokenRepo) ByLookup(ctx context.Context, lookup string) (store.Token, error) {
	row, err := r.q.GetTokenByLookup(ctx, lookup)
	if err != nil {
		return store.Token{}, mapReadError("getting token by lookup", err)
	}
	return toStoreToken(row)
}

// ConsumeEnrollment atomically marks the enrollment token used and creates
// dev in one transaction: the UPDATE's WHERE used_at IS NULL guard is what
// makes two concurrent callers agree on exactly one winner (design's
// Interfaces section, storetest's ConsumeEnrollmentIsAtomic case). The
// loser observes zero rows affected on an existing token and returns
// ErrConflict, never a second device.
func (r tokenRepo) ConsumeEnrollment(ctx context.Context, lookup string, dev domain.Device) (domain.Device, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Device{}, fmt.Errorf("store: consuming enrollment token: %w", err)
	}
	defer tx.Rollback()
	q := r.q.WithTx(tx)

	tok, err := q.GetTokenByLookup(ctx, lookup)
	if err != nil {
		return domain.Device{}, mapReadError("consuming enrollment token", err)
	}

	if dev.ID == "" {
		dev.ID = uuid.NewString()
	}
	nowText := formatTime(time.Now())

	// ExpiresAt here is the "now" the WHERE clause compares expires_at
	// against, not a new value being written — the query only ever sets
	// used_at and subject_id.
	affected, err := q.ConsumeEnrollmentToken(ctx, ConsumeEnrollmentTokenParams{
		UsedAt: &nowText, SubjectID: dev.ID, Lookup: lookup, ExpiresAt: &nowText,
	})
	if err != nil {
		return domain.Device{}, fmt.Errorf("store: consuming enrollment token: %w", err)
	}
	if affected == 0 {
		return domain.Device{}, fmt.Errorf("consuming enrollment token: %w", store.ErrConflict)
	}

	if err := q.InsertDevice(ctx, InsertDeviceParams{
		ID: dev.ID, Name: dev.Name, Platform: dev.Platform.String(), Shell: dev.Shell.String(),
		ClientVersion: dev.ClientVersion, CreatedAt: nowText, UpdatedAt: nowText,
	}); err != nil {
		return domain.Device{}, mapWriteError("consuming enrollment token", err)
	}

	profileIDs, err := decodeStrings(tok.ProfileIds)
	if err != nil {
		return domain.Device{}, err
	}
	for _, profileID := range profileIDs {
		if err := q.InsertDeviceProfile(ctx, InsertDeviceProfileParams{DeviceID: dev.ID, ProfileID: profileID}); err != nil {
			return domain.Device{}, mapWriteError("consuming enrollment token", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return domain.Device{}, fmt.Errorf("store: consuming enrollment token: %w", err)
	}

	dev.ProfileIDs = profileIDs
	return dev, nil
}

func (r tokenRepo) Revoke(ctx context.Context, id string, at time.Time) error {
	revokedAt := formatTime(at)
	rows, err := r.q.RevokeToken(ctx, RevokeTokenParams{RevokedAt: &revokedAt, ID: id})
	if err != nil {
		return fmt.Errorf("store: revoking token: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("revoking token: %w", store.ErrNotFound)
	}
	return nil
}

func (r tokenRepo) RevokeSubject(ctx context.Context, kind store.TokenKind, subjectID string, at time.Time) error {
	revokedAt := formatTime(at)
	err := r.q.RevokeTokensBySubject(ctx, RevokeTokensBySubjectParams{
		RevokedAt: &revokedAt, Kind: string(kind), SubjectID: subjectID,
	})
	if err != nil {
		return fmt.Errorf("store: revoking tokens by subject: %w", err)
	}
	return nil
}

func toStoreToken(row Token) (store.Token, error) {
	profileIDs, err := decodeStrings(row.ProfileIds)
	if err != nil {
		return store.Token{}, err
	}
	createdAt, err := parseTime(row.CreatedAt)
	if err != nil {
		return store.Token{}, fmt.Errorf("store: parsing token created_at: %w", err)
	}
	return store.Token{
		ID:         row.ID,
		Lookup:     row.Lookup,
		Kind:       store.TokenKind(row.Kind),
		SubjectID:  row.SubjectID,
		SecretHash: row.SecretHash,
		ProfileIDs: profileIDs,
		CreatedAt:  createdAt,
		ExpiresAt:  parseNullableTime(row.ExpiresAt),
		UsedAt:     parseNullableTime(row.UsedAt),
		RevokedAt:  parseNullableTime(row.RevokedAt),
	}, nil
}

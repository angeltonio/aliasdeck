package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// deviceRepo implements store.DeviceRepo. There is no Create: a device is
// born only through tokenRepo.ConsumeEnrollment (see tokens.go), which is
// what makes enrollment atomic.
type deviceRepo struct {
	db *sql.DB
	q  *Queries
}

func (r deviceRepo) Get(ctx context.Context, id string) (domain.Device, error) {
	row, err := r.q.GetDevice(ctx, id)
	if err != nil {
		return domain.Device{}, mapReadError("getting device", err)
	}
	profileIDs, err := r.q.ListDeviceProfileIDs(ctx, id)
	if err != nil {
		return domain.Device{}, fmt.Errorf("store: getting device: %w", err)
	}
	return toDomainDevice(row, profileIDs), nil
}

func (r deviceRepo) List(ctx context.Context) ([]domain.Device, error) {
	rows, err := r.q.ListDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing devices: %w", err)
	}
	out := make([]domain.Device, 0, len(rows))
	for _, row := range rows {
		profileIDs, err := r.q.ListDeviceProfileIDs(ctx, row.ID)
		if err != nil {
			return nil, fmt.Errorf("store: listing devices: %w", err)
		}
		out = append(out, toDomainDevice(row, profileIDs))
	}
	return out, nil
}

func (r deviceRepo) Update(ctx context.Context, d domain.Device) (domain.Device, error) {
	now := formatTime(time.Now())

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Device{}, fmt.Errorf("store: updating device: %w", err)
	}
	defer tx.Rollback()
	q := r.q.WithTx(tx)

	rows, err := q.UpdateDevice(ctx, UpdateDeviceParams{Name: d.Name, UpdatedAt: now, ID: d.ID})
	if err != nil {
		return domain.Device{}, fmt.Errorf("store: updating device: %w", err)
	}
	if rows == 0 {
		return domain.Device{}, fmt.Errorf("updating device: %w", store.ErrNotFound)
	}

	if err := setDeviceProfiles(ctx, q, d.ID, d.ProfileIDs); err != nil {
		return domain.Device{}, err
	}

	updated, err := q.GetDevice(ctx, d.ID)
	if err != nil {
		return domain.Device{}, fmt.Errorf("store: updating device: %w", err)
	}
	profileIDs, err := q.ListDeviceProfileIDs(ctx, d.ID)
	if err != nil {
		return domain.Device{}, fmt.Errorf("store: updating device: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return domain.Device{}, fmt.Errorf("store: updating device: %w", err)
	}

	return toDomainDevice(updated, profileIDs), nil
}

func (r deviceRepo) Delete(ctx context.Context, id string) error {
	rows, err := r.q.DeleteDevice(ctx, id)
	if err != nil {
		return fmt.Errorf("store: deleting device: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("deleting device: %w", store.ErrNotFound)
	}
	return nil
}

// Touch records the platform/shell a sync GET reported and stamps
// last_seen_at/last_sync_at (design decision 10).
func (r deviceRepo) Touch(ctx context.Context, id string, platform domain.Platform, shell domain.Shell, at time.Time) error {
	stamp := formatTime(at)
	rows, err := r.q.TouchDevice(ctx, TouchDeviceParams{
		Platform: platform.String(), Shell: shell.String(),
		LastSeenAt: &stamp, LastSyncAt: &stamp, UpdatedAt: formatTime(time.Now()), ID: id,
	})
	if err != nil {
		return fmt.Errorf("store: touching device: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("touching device: %w", store.ErrNotFound)
	}
	return nil
}

// Heartbeat records device reachability without changing sync-specific
// bookkeeping or the platform/shell last reported by a sync.
func (r deviceRepo) Heartbeat(ctx context.Context, id string, at time.Time) error {
	stamp := formatTime(at)
	rows, err := r.q.HeartbeatDevice(ctx, HeartbeatDeviceParams{
		LastSeenAt: &stamp,
		UpdatedAt:  formatTime(time.Now()),
		ID:         id,
	})
	if err != nil {
		return fmt.Errorf("store: recording device heartbeat: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("recording device heartbeat: %w", store.ErrNotFound)
	}
	return nil
}

func (r deviceRepo) Revoke(ctx context.Context, id string, at time.Time) error {
	revokedAt := formatTime(at)
	rows, err := r.q.RevokeDevice(ctx, RevokeDeviceParams{
		RevokedAt: &revokedAt, UpdatedAt: formatTime(time.Now()), ID: id,
	})
	if err != nil {
		return fmt.Errorf("store: revoking device: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("revoking device: %w", store.ErrNotFound)
	}
	return nil
}

func setDeviceProfiles(ctx context.Context, q *Queries, deviceID string, profileIDs []string) error {
	if err := q.ClearDeviceProfiles(ctx, deviceID); err != nil {
		return fmt.Errorf("store: setting device profile membership: %w", err)
	}
	for _, profileID := range profileIDs {
		if err := q.InsertDeviceProfile(ctx, InsertDeviceProfileParams{DeviceID: deviceID, ProfileID: profileID}); err != nil {
			return mapWriteError("setting device profile membership", err)
		}
	}
	return nil
}

func toDomainDevice(row Device, profileIDs []string) domain.Device {
	return domain.Device{
		ID:            row.ID,
		Name:          row.Name,
		Platform:      domain.Platform(row.Platform),
		Shell:         domain.Shell(row.Shell),
		ClientVersion: row.ClientVersion,
		ProfileIDs:    profileIDs,
		LastSeenAt:    parseNullableTimePtr(row.LastSeenAt),
		LastSyncAt:    parseNullableTimePtr(row.LastSyncAt),
	}
}

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/google/uuid"
)

// aliasRepo implements store.AliasRepo. It keeps its own *sql.DB reference
// (rather than only *Queries) because Create and Update touch the alias row
// and its two targeting join tables together, and that has to happen in one
// transaction — a cancelled context or a mid-write failure must leave
// nothing behind.
type aliasRepo struct {
	db *sql.DB
	q  *Queries
}

func (r aliasRepo) Create(ctx context.Context, a domain.Alias) (domain.Alias, error) {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}

	platforms, err := encodePlatforms(a.Platforms)
	if err != nil {
		return domain.Alias{}, err
	}
	shells, err := encodeShells(a.Shells)
	if err != nil {
		return domain.Alias{}, err
	}
	tags, err := encodeStrings(a.Tags)
	if err != nil {
		return domain.Alias{}, err
	}
	now := formatTime(time.Now())

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Alias{}, fmt.Errorf("store: creating alias: %w", err)
	}
	defer tx.Rollback()
	q := r.q.WithTx(tx)

	row, err := q.CreateAlias(ctx, CreateAliasParams{
		ID: a.ID, Name: a.Name, Command: a.Command, Description: a.Description,
		Enabled: boolToInt64(a.Enabled), Platforms: platforms, Shells: shells, Tags: tags,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return domain.Alias{}, mapWriteError("creating alias", err)
	}

	if err := setAliasProfiles(ctx, q, a.ID, a.ProfileIDs); err != nil {
		return domain.Alias{}, err
	}
	if err := setAliasDevices(ctx, q, a.ID, a.DeviceIDs); err != nil {
		return domain.Alias{}, err
	}

	profileIDs, err := q.ListAliasProfileIDs(ctx, a.ID)
	if err != nil {
		return domain.Alias{}, fmt.Errorf("store: creating alias: %w", err)
	}
	deviceIDs, err := q.ListAliasDeviceIDs(ctx, a.ID)
	if err != nil {
		return domain.Alias{}, fmt.Errorf("store: creating alias: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return domain.Alias{}, fmt.Errorf("store: creating alias: %w", err)
	}

	return toDomainAlias(row, profileIDs, deviceIDs)
}

func (r aliasRepo) Get(ctx context.Context, id string) (domain.Alias, error) {
	row, err := r.q.GetAlias(ctx, id)
	if err != nil {
		return domain.Alias{}, mapReadError("getting alias", err)
	}
	profileIDs, err := r.q.ListAliasProfileIDs(ctx, id)
	if err != nil {
		return domain.Alias{}, fmt.Errorf("store: getting alias: %w", err)
	}
	deviceIDs, err := r.q.ListAliasDeviceIDs(ctx, id)
	if err != nil {
		return domain.Alias{}, fmt.Errorf("store: getting alias: %w", err)
	}
	return toDomainAlias(row, profileIDs, deviceIDs)
}

func (r aliasRepo) List(ctx context.Context) ([]domain.Alias, error) {
	rows, err := r.q.ListAliases(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing aliases: %w", err)
	}

	out := make([]domain.Alias, 0, len(rows))
	for _, row := range rows {
		profileIDs, err := r.q.ListAliasProfileIDs(ctx, row.ID)
		if err != nil {
			return nil, fmt.Errorf("store: listing aliases: %w", err)
		}
		deviceIDs, err := r.q.ListAliasDeviceIDs(ctx, row.ID)
		if err != nil {
			return nil, fmt.Errorf("store: listing aliases: %w", err)
		}
		a, err := toDomainAlias(row, profileIDs, deviceIDs)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (r aliasRepo) Update(ctx context.Context, a domain.Alias) (domain.Alias, error) {
	platforms, err := encodePlatforms(a.Platforms)
	if err != nil {
		return domain.Alias{}, err
	}
	shells, err := encodeShells(a.Shells)
	if err != nil {
		return domain.Alias{}, err
	}
	tags, err := encodeStrings(a.Tags)
	if err != nil {
		return domain.Alias{}, err
	}
	now := formatTime(time.Now())

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Alias{}, fmt.Errorf("store: updating alias: %w", err)
	}
	defer tx.Rollback()
	q := r.q.WithTx(tx)

	row, err := q.UpdateAlias(ctx, UpdateAliasParams{
		Name: a.Name, Command: a.Command, Description: a.Description,
		Enabled: boolToInt64(a.Enabled), Platforms: platforms, Shells: shells, Tags: tags,
		UpdatedAt: now, ID: a.ID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Alias{}, fmt.Errorf("updating alias: %w", store.ErrNotFound)
		}
		return domain.Alias{}, mapWriteError("updating alias", err)
	}

	if err := setAliasProfiles(ctx, q, a.ID, a.ProfileIDs); err != nil {
		return domain.Alias{}, err
	}
	if err := setAliasDevices(ctx, q, a.ID, a.DeviceIDs); err != nil {
		return domain.Alias{}, err
	}

	profileIDs, err := q.ListAliasProfileIDs(ctx, a.ID)
	if err != nil {
		return domain.Alias{}, fmt.Errorf("store: updating alias: %w", err)
	}
	deviceIDs, err := q.ListAliasDeviceIDs(ctx, a.ID)
	if err != nil {
		return domain.Alias{}, fmt.Errorf("store: updating alias: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return domain.Alias{}, fmt.Errorf("store: updating alias: %w", err)
	}

	return toDomainAlias(row, profileIDs, deviceIDs)
}

// Delete relies on ON DELETE CASCADE (design decision 7's foreign_keys=on
// pragma) to remove the alias's alias_profiles/alias_devices rows; no
// explicit cleanup query is needed here.
func (r aliasRepo) Delete(ctx context.Context, id string) error {
	rows, err := r.q.DeleteAlias(ctx, id)
	if err != nil {
		return fmt.Errorf("store: deleting alias: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("deleting alias: %w", store.ErrNotFound)
	}
	return nil
}

func setAliasProfiles(ctx context.Context, q *Queries, aliasID string, profileIDs []string) error {
	if err := q.ClearAliasProfiles(ctx, aliasID); err != nil {
		return fmt.Errorf("store: setting alias profile targeting: %w", err)
	}
	for _, profileID := range profileIDs {
		if err := q.InsertAliasProfile(ctx, InsertAliasProfileParams{AliasID: aliasID, ProfileID: profileID}); err != nil {
			return mapWriteError("setting alias profile targeting", err)
		}
	}
	return nil
}

func setAliasDevices(ctx context.Context, q *Queries, aliasID string, deviceIDs []string) error {
	if err := q.ClearAliasDevices(ctx, aliasID); err != nil {
		return fmt.Errorf("store: setting alias device targeting: %w", err)
	}
	for _, deviceID := range deviceIDs {
		if err := q.InsertAliasDevice(ctx, InsertAliasDeviceParams{AliasID: aliasID, DeviceID: deviceID}); err != nil {
			return mapWriteError("setting alias device targeting", err)
		}
	}
	return nil
}

func toDomainAlias(row Alias, profileIDs, deviceIDs []string) (domain.Alias, error) {
	platforms, err := decodePlatforms(row.Platforms)
	if err != nil {
		return domain.Alias{}, err
	}
	shells, err := decodeShells(row.Shells)
	if err != nil {
		return domain.Alias{}, err
	}
	tags, err := decodeStrings(row.Tags)
	if err != nil {
		return domain.Alias{}, err
	}
	createdAt, err := parseTime(row.CreatedAt)
	if err != nil {
		return domain.Alias{}, fmt.Errorf("store: parsing alias created_at: %w", err)
	}
	updatedAt, err := parseTime(row.UpdatedAt)
	if err != nil {
		return domain.Alias{}, fmt.Errorf("store: parsing alias updated_at: %w", err)
	}

	return domain.Alias{
		ID:          row.ID,
		Name:        row.Name,
		Command:     row.Command,
		Description: row.Description,
		Enabled:     int64ToBool(row.Enabled),
		Tags:        tags,
		Platforms:   platforms,
		Shells:      shells,
		ProfileIDs:  profileIDs,
		DeviceIDs:   deviceIDs,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

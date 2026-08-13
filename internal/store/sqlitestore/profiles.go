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

type profileRepo struct {
	q *Queries
}

func (r profileRepo) Create(ctx context.Context, p domain.Profile) (domain.Profile, error) {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	now := formatTime(time.Now())

	row, err := r.q.CreateProfile(ctx, CreateProfileParams{
		ID: p.ID, Name: p.Name, Description: p.Description, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return domain.Profile{}, mapWriteError("creating profile", err)
	}
	return toDomainProfile(row)
}

func (r profileRepo) Get(ctx context.Context, id string) (domain.Profile, error) {
	row, err := r.q.GetProfile(ctx, id)
	if err != nil {
		return domain.Profile{}, mapReadError("getting profile", err)
	}
	return toDomainProfile(row)
}

func (r profileRepo) List(ctx context.Context) ([]domain.Profile, error) {
	rows, err := r.q.ListProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: listing profiles: %w", err)
	}
	out := make([]domain.Profile, 0, len(rows))
	for _, row := range rows {
		p, err := toDomainProfile(row)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (r profileRepo) Update(ctx context.Context, p domain.Profile) (domain.Profile, error) {
	now := formatTime(time.Now())
	row, err := r.q.UpdateProfile(ctx, UpdateProfileParams{
		Name: p.Name, Description: p.Description, UpdatedAt: now, ID: p.ID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Profile{}, fmt.Errorf("updating profile: %w", store.ErrNotFound)
		}
		return domain.Profile{}, mapWriteError("updating profile", err)
	}
	return toDomainProfile(row)
}

// Delete relies on ON DELETE CASCADE (design decision 7's foreign_keys=on
// pragma) to remove the profile's alias_profiles/device_profiles rows.
func (r profileRepo) Delete(ctx context.Context, id string) error {
	rows, err := r.q.DeleteProfile(ctx, id)
	if err != nil {
		return fmt.Errorf("store: deleting profile: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("deleting profile: %w", store.ErrNotFound)
	}
	return nil
}

func toDomainProfile(row Profile) (domain.Profile, error) {
	createdAt, err := parseTime(row.CreatedAt)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("store: parsing profile created_at: %w", err)
	}
	updatedAt, err := parseTime(row.UpdatedAt)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("store: parsing profile updated_at: %w", err)
	}
	return domain.Profile{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

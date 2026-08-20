package sqlitestore

import (
	"context"
	"fmt"
	"time"

	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/google/uuid"
)

type operatorRepo struct {
	q *Queries
}

func (r operatorRepo) Create(ctx context.Context, o store.Operator) (store.Operator, error) {
	if o.ID == "" {
		o.ID = uuid.NewString()
	}
	now := formatTime(time.Now())

	row, err := r.q.CreateOperator(ctx, CreateOperatorParams{
		ID: o.ID, Username: o.Username, PasswordHash: string(o.PasswordHash), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return store.Operator{}, mapWriteError("creating operator", err)
	}
	return toStoreOperator(row)
}

func (r operatorRepo) Get(ctx context.Context, id string) (store.Operator, error) {
	row, err := r.q.GetOperator(ctx, id)
	if err != nil {
		return store.Operator{}, mapReadError("getting operator", err)
	}
	return toStoreOperator(row)
}

func (r operatorRepo) ByUsername(ctx context.Context, username string) (store.Operator, error) {
	row, err := r.q.GetOperatorByUsername(ctx, username)
	if err != nil {
		return store.Operator{}, mapReadError("getting operator by username", err)
	}
	return toStoreOperator(row)
}

func (r operatorRepo) Count(ctx context.Context) (int, error) {
	n, err := r.q.CountOperators(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: counting operators: %w", err)
	}
	return int(n), nil
}

func (r operatorRepo) UpdatePasswordHash(ctx context.Context, username string, hash []byte) (store.Operator, error) {
	row, err := r.q.UpdateOperatorPassword(ctx, UpdateOperatorPasswordParams{
		PasswordHash: string(hash), UpdatedAt: formatTime(time.Now()), Username: username,
	})
	if err != nil {
		// A username that matches no row yields sql.ErrNoRows from the
		// RETURNING clause rather than a write failure, so this reads as
		// ErrNotFound the same way Get and ByUsername do.
		return store.Operator{}, mapReadError("updating operator password", err)
	}
	return toStoreOperator(row)
}

func toStoreOperator(row Operator) (store.Operator, error) {
	createdAt, err := parseTime(row.CreatedAt)
	if err != nil {
		return store.Operator{}, fmt.Errorf("store: parsing operator created_at: %w", err)
	}
	updatedAt, err := parseTime(row.UpdatedAt)
	if err != nil {
		return store.Operator{}, fmt.Errorf("store: parsing operator updated_at: %w", err)
	}
	return store.Operator{
		ID:           row.ID,
		Username:     row.Username,
		PasswordHash: []byte(row.PasswordHash),
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}, nil
}

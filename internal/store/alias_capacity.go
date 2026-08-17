package store

import (
	"context"
	"fmt"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// CreateAliasWithinLimit prefers an atomic store-backed bounded insert. The
// List/Create fallback keeps lightweight test and third-party repositories
// compatible, while the shipped SQLite store implements BoundedAliasCreator.
func CreateAliasWithinLimit(ctx context.Context, repo AliasRepo, a domain.Alias, limit int) (domain.Alias, error) {
	if bounded, ok := repo.(BoundedAliasCreator); ok {
		return bounded.CreateWithinLimit(ctx, a, limit)
	}
	existing, err := repo.List(ctx)
	if err != nil {
		return domain.Alias{}, err
	}
	if len(existing) >= limit {
		return domain.Alias{}, fmt.Errorf("store: creating alias: %w", ErrCapacity)
	}
	return repo.Create(ctx, a)
}

package server

import (
	"context"
	"sync"

	"github.com/angeltonio/aliasdeck/internal/store"
)

// fakeStore is a minimal store.Store double used to drive Run's own
// composition logic (bootstrap wiring, shutdown, ordering) without a real
// SQLite file. Its Operators() repo always reports one existing operator,
// so auth.Bootstrap no-ops immediately — these tests are about Run's
// control flow, not Bootstrap's own behavior (that is
// internal/auth/bootstrap_test.go's job).
type fakeStore struct {
	mu     sync.Mutex
	closed bool
}

func (f *fakeStore) Aliases() store.AliasRepo      { return nil }
func (f *fakeStore) Devices() store.DeviceRepo     { return nil }
func (f *fakeStore) Profiles() store.ProfileRepo   { return nil }
func (f *fakeStore) Tokens() store.TokenRepo       { return nil }
func (f *fakeStore) Operators() store.OperatorRepo { return fakeOperatorRepo{} }

func (f *fakeStore) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeStore) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// fakeOperatorRepo reports one operator already exists, so
// auth.Bootstrap.Count short-circuits and never calls Create.
type fakeOperatorRepo struct{}

func (fakeOperatorRepo) Create(_ context.Context, o store.Operator) (store.Operator, error) {
	return o, nil
}

func (fakeOperatorRepo) Get(_ context.Context, _ string) (store.Operator, error) {
	return store.Operator{}, store.ErrNotFound
}

func (fakeOperatorRepo) ByUsername(_ context.Context, _ string) (store.Operator, error) {
	return store.Operator{}, store.ErrNotFound
}

func (fakeOperatorRepo) Count(_ context.Context) (int, error) {
	return 1, nil
}

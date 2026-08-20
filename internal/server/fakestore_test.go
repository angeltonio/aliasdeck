package server

import (
	"context"
	"sync"

	"github.com/angeltonio/aliasdeck/internal/store"
)

// fakeStore is a minimal store.Store double used to drive Run's own
// composition logic (bootstrap wiring, shutdown, ordering) without a real
// SQLite file. Its zero value's Operators() repo reports one existing
// operator, so auth.Bootstrap no-ops immediately — most of these tests are
// about Run's control flow, not Bootstrap's own behavior (that is
// internal/auth/bootstrap_test.go's job). A subset of tests need Run's
// actual Bootstrap call — both its success and its error path — covered
// too (bounded-review finding: the zero-value fake's Count() always
// returning 1 left both paths of that call unexercised); operatorCount and
// createErr let a test opt into that without inventing a second fake type.
type fakeStore struct {
	mu     sync.Mutex
	closed bool

	// emptyOperators, set via newFakeStoreWithEmptyOperators or
	// newFakeStoreWithOperatorCreateError, makes Operators().Count report
	// 0 instead of the zero-value default of 1, so a test can drive
	// auth.Bootstrap's Create branch instead of the always-no-op default.
	emptyOperators bool
	// createErr, when non-nil, is what Operators().Create returns instead
	// of succeeding — the uncovered branch of Run's Bootstrap call.
	createErr error
	// createCalled records whether Operators().Create was actually
	// invoked, so a test can prove Run really reached it rather than only
	// observing that Run happened to return successfully for some other
	// reason (e.g. Run's auth.Bootstrap call being skipped entirely).
	createCalled bool
}

// newFakeStoreWithEmptyOperators returns a fakeStore whose Operators()
// reports no existing operator, so Run's auth.Bootstrap call actually
// exercises Create instead of no-oping on the zero-value fake's count of 1.
func newFakeStoreWithEmptyOperators() *fakeStore {
	return &fakeStore{emptyOperators: true}
}

// newFakeStoreWithOperatorCreateError is
// newFakeStoreWithEmptyOperators, plus a forced Create failure — the
// bootstrap error path Run's "server: bootstrapping operator: %w" wraps.
func newFakeStoreWithOperatorCreateError(err error) *fakeStore {
	return &fakeStore{emptyOperators: true, createErr: err}
}

func (f *fakeStore) Aliases() store.AliasRepo    { return nil }
func (f *fakeStore) Devices() store.DeviceRepo   { return nil }
func (f *fakeStore) Profiles() store.ProfileRepo { return nil }
func (f *fakeStore) Tokens() store.TokenRepo     { return nil }
func (f *fakeStore) Operators() store.OperatorRepo {
	count := 1
	if f.emptyOperators {
		count = 0
	}
	return fakeOperatorRepo{count: count, createErr: f.createErr, onCreate: f.markCreateCalled}
}

func (f *fakeStore) markCreateCalled() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalled = true
}

// createWasCalled reports whether Operators().Create was actually invoked
// on this store — the assertion TestRunBootstrapsOperatorOnEmptyStore and
// TestRunWrapsBootstrapErrorFromOperatorCreate both need to prove Run
// really reached auth.Bootstrap's Create call, not merely that Run
// returned the expected result for some unrelated reason.
func (f *fakeStore) createWasCalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createCalled
}

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

// fakeOperatorRepo reports count existing operators — 1 by default, so
// auth.Bootstrap.Count short-circuits and never calls Create, or 0 when a
// test opts in via newFakeStoreWithEmptyOperators.
type fakeOperatorRepo struct {
	count     int
	createErr error
	onCreate  func()
}

func (f fakeOperatorRepo) Create(_ context.Context, o store.Operator) (store.Operator, error) {
	if f.onCreate != nil {
		f.onCreate()
	}
	if f.createErr != nil {
		return store.Operator{}, f.createErr
	}
	return o, nil
}

func (fakeOperatorRepo) Get(_ context.Context, _ string) (store.Operator, error) {
	return store.Operator{}, store.ErrNotFound
}

func (fakeOperatorRepo) ByUsername(_ context.Context, _ string) (store.Operator, error) {
	return store.Operator{}, store.ErrNotFound
}

func (f fakeOperatorRepo) Count(_ context.Context) (int, error) {
	return f.count, nil
}

// This fake keeps no operator rows at all — it exists to drive
// auth.Bootstrap's Count branch — so there is never a record to update.
func (fakeOperatorRepo) UpdatePasswordHash(_ context.Context, _ string, _ []byte) (store.Operator, error) {
	return store.Operator{}, store.ErrNotFound
}

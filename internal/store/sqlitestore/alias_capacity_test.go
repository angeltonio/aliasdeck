package sqlitestore

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
)

func TestCreateAliasWithinLimitIsAtomic(t *testing.T) {
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "aliasdeck.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bounded := st.Aliases().(store.BoundedAliasCreator)

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, name := range []string{"one", "two"} {
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := bounded.CreateWithinLimit(context.Background(), domain.Alias{Name: name, Command: "true", Enabled: true}, 1)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	var successes, capacity int
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, store.ErrCapacity):
			capacity++
		default:
			t.Fatalf("unexpected bounded create error: %v", err)
		}
	}
	if successes != 1 || capacity != 1 {
		t.Fatalf("bounded creates: successes=%d capacity=%d, want 1/1", successes, capacity)
	}
	list, err := st.Aliases().List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("stored aliases=%v err=%v, want exactly one", list, err)
	}
}

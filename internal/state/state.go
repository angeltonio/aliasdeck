// Package state persists the local record of a device's last successful
// sync: the revision applied, a hash of the generated file, and — when the
// shell rc file has been bootstrapped — the exact bytes appended to it.
//
// state.json is machine-owned and never hand-edited (design decision 4): a
// missing or corrupt file degrades to an empty State rather than a fatal
// error, so a damaged or deleted state.json never blocks the next sync.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
)

// State is the local sync-state record for one device (sync-state spec,
// "Sync State Is Recorded After Apply").
type State struct {
	Version       int             `json:"version"` // 1
	Revision      string          `json:"revision"`
	OutputPath    string          `json:"outputPath"`
	OutputHash    string          `json:"outputHash"` // sha256 hex of rendered bytes
	AliasCount    int             `json:"aliasCount"`
	Platform      domain.Platform `json:"platform"`
	Shell         domain.Shell    `json:"shell"`
	SourceType    string          `json:"sourceType"`
	SourceRef     string          `json:"sourceRef"`
	LastSyncAt    time.Time       `json:"lastSyncAt"`
	ClientVersion string          `json:"clientVersion"`
	Bootstrap     *Bootstrap      `json:"bootstrap,omitempty"`
}

// Bootstrap records the shell rc file bootstrap AliasDeck performed, if any.
//
// Block holds the exact appended bytes AddBootstrap returned — including any
// leading padding or separator — so uninstall's removal is a single
// bytes.Replace against these stored bytes rather than a reconstruction
// (design decision 6).
type Bootstrap struct {
	RCPath  string    `json:"rcPath"`
	Block   string    `json:"block"`
	RCHash  string    `json:"rcHash"`
	AddedAt time.Time `json:"addedAt"`
}

// Load reads state.json at path.
//
// A missing file or one that fails to parse as valid State JSON both degrade
// to a zero State with a nil error: state is a cache of the last successful
// sync, not a source of truth, and refusing to sync because state.json got
// corrupted would be worse than simply resyncing from scratch. Any other
// read failure (e.g. a permission error) is still reported, since that is
// not the class of problem this tolerance is meant to absorb.
func Load(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, nil
	}
	return s, nil
}

// Save writes s to path as state.json, atomically and at mode 0600: a temp
// file in the same directory, then a rename, so a concurrent Load never
// observes a partially written file, followed by a deferred cleanup of the
// temp file that is a no-op once the rename has succeeded.
func Save(path string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".state.*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := writeSyncCloseState(tmp, data); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmpPath, path, err)
	}
	return nil
}

func writeSyncCloseState(f *os.File, data []byte) error {
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return fmt.Errorf("setting mode on %s: %w", f.Name(), err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", f.Name(), err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("syncing %s: %w", f.Name(), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", f.Name(), err)
	}
	return nil
}

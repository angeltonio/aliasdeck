package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credentials is a device's server enrollment credential, stored at
// CredentialsFile(base) rather than inside config.yaml (design decision 14;
// server-source spec, "Device Token Stored Outside config.yaml"). It is
// never written into config.yaml or state.json, both of which users are
// encouraged to inspect and paste into an issue.
type Credentials struct {
	Version     int       `json:"version"` // 1
	ServerURL   string    `json:"serverUrl"`
	DeviceID    string    `json:"deviceId"`
	DeviceToken string    `json:"deviceToken"`
	ObtainedAt  time.Time `json:"obtainedAt"`
}

// LoadCredentials reads path. A missing file returns a zero Credentials and
// a nil error, mirroring state.Load's tolerance for "nothing recorded yet"
// — there is no credential before the first successful `register`. Corrupt
// JSON is a real error, though, unlike state.Load's tolerance for a broken
// state.json: a credentials file holds a live, server-issued token this
// process did not mint and cannot safely regenerate, so a caller must not
// treat corruption as an empty, silently-reset credential.
func LoadCredentials(path string) (Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Credentials{}, nil
		}
		return Credentials{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return Credentials{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return c, nil
}

// SaveCredentials writes c to path atomically and at mode 0600: a temp file
// in the same directory, chmod'd before any content touches it, then a
// rename — the same pattern internal/state.Save and
// internal/auth.writeBootstrapPasswordFile already use for a live secret, so
// this is that pattern's third call site rather than a new one (design
// decision 14). A deferred removal of the temp file cleans up after any
// failure between its creation and the rename; it is a no-op once the
// rename has already succeeded.
func SaveCredentials(path string, c Credentials) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling credentials: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".credentials.*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := writeSyncCloseCredentials(tmp, data); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmpPath, path, err)
	}
	return nil
}

func writeSyncCloseCredentials(f *os.File, data []byte) error {
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

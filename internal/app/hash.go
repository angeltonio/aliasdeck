package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// hashBytes returns the sha256 hex digest of data (state.State.OutputHash's
// format, design's Interfaces section).
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// diskHashMatches reports whether the file at path currently hashes to
// want. Any read failure (including a missing file) reports false: an
// unreadable or deleted generated file is never considered "still in
// sync" (sync-state spec, "On-disk file manually altered").
func diskHashMatches(path, want string) bool {
	if want == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return hashBytes(data) == want
}

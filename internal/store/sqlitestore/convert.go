package sqlitestore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/store"
	sqlitedriver "modernc.org/sqlite"
)

// sqliteConstraintPrimaryCode is SQLITE_CONSTRAINT's primary result code
// (the low byte of both the plain and every "extended" constraint code,
// e.g. SQLITE_CONSTRAINT_UNIQUE). Masking with 0xff is the documented way
// to recover the primary code from an extended one.
const sqliteConstraintPrimaryCode = 19

// sqliteConstraintForeignKey is SQLITE_CONSTRAINT_FOREIGNKEY, the extended
// result code modernc.org/sqlite's Error.Code() returns for a foreign-key
// violation. It must be checked before the 0xff mask below: masking it
// first collapses it to the same primary code (19) every other
// SQLITE_CONSTRAINT_* subtype shares, which is exactly the bug design
// decision 18 fixes — a dangling reference (e.g. an alias targeting a
// profile ID that does not exist) is not a name collision and must not
// surface as ErrConflict.
const sqliteConstraintForeignKey = 787

// mapWriteError translates a raw *sql.DB error into a store sentinel
// (design decision 18): a foreign-key violation becomes ErrInvalidReference,
// any other constraint violation (UNIQUE, NOT NULL, ...) becomes
// ErrConflict, everything else passes through wrapped with context. This is
// the one place sqlitestore looks at a driver-specific error type — the
// store interfaces themselves never see it (design decision 3).
func mapWriteError(op string, err error) error {
	if err == nil {
		return nil
	}
	var sqliteErr *sqlitedriver.Error
	if errors.As(err, &sqliteErr) {
		switch {
		case sqliteErr.Code() == sqliteConstraintForeignKey:
			return fmt.Errorf("%s: %w", op, store.ErrInvalidReference)
		case sqliteErr.Code()&0xff == sqliteConstraintPrimaryCode:
			return fmt.Errorf("%s: %w", op, store.ErrConflict)
		}
	}
	return fmt.Errorf("store: %s: %w", op, err)
}

// mapReadError translates sql.ErrNoRows into store.ErrNotFound.
func mapReadError(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", op, store.ErrNotFound)
	}
	return fmt.Errorf("store: %s: %w", op, err)
}

// timeFormat is RFC 3339 with a fixed nine-digit fractional second and a
// zone offset that is always "Z" (formatTime always calls t.UTC() first).
// This is deliberately NOT time.RFC3339Nano: that layout's "9" fractional
// digits are trimmed when zero, so "2024-01-01T00:00:00Z" (no fraction) and
// "2024-01-01T00:00:00.5Z" (half a second later) are different widths, and
// "." (0x2E) sorts before "Z" (0x5A) in the whole-second string — text
// comparison then disagrees with chronological order in both directions.
// The "0" fractional digits here are zero-padded rather than trimmed, so
// every formatted timestamp is exactly the same width and lexicographic
// order matches chronological order — which is what query.sql's
// expires_at > ? guard (and any future ORDER BY over a time column) relies
// on, since sqlite has no native timestamp type and compares this column as
// TEXT.
const timeFormat = "2006-01-02T15:04:05.000000000Z07:00"

func formatTime(t time.Time) string {
	return t.UTC().Format(timeFormat)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(timeFormat, s)
}

// formatNullableTime returns nil for the zero time, matching the "zero
// means never/unset" convention store.Token and domain.Device use for
// their optional timestamp fields.
func formatNullableTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := formatTime(t)
	return &s
}

// parseNullableTime is formatNullableTime's inverse for a value scanned
// into a *string; nil or an unparsable value both yield the zero time
// rather than failing the caller, since these fields are optional at rest.
func parseNullableTime(s *string) time.Time {
	if s == nil {
		return time.Time{}
	}
	t, err := parseTime(*s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// parseNullableTimePtr is parseNullableTime's *time.Time counterpart, used
// for domain.Device's LastSeenAt/LastSyncAt fields.
func parseNullableTimePtr(s *string) *time.Time {
	if s == nil {
		return nil
	}
	t := parseNullableTime(s)
	return &t
}

func formatNullableTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return formatNullableTime(*t)
}

// encodeStrings and decodeStrings round-trip a []string through the JSON
// text columns (platforms, shells, tags, profile_ids): domain.Resolve is
// their only reader, so normalizing them into join tables would buy query
// shapes design decision 4 forbids.
func encodeStrings(values []string) (string, error) {
	if len(values) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("store: encoding %v as JSON: %w", values, err)
	}
	return string(b), nil
}

func decodeStrings(text string) ([]string, error) {
	if text == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(text), &values); err != nil {
		return nil, fmt.Errorf("store: decoding %q as JSON: %w", text, err)
	}
	return values, nil
}

func encodePlatforms(platforms []domain.Platform) (string, error) {
	strs := make([]string, len(platforms))
	for i, p := range platforms {
		strs[i] = p.String()
	}
	return encodeStrings(strs)
}

func decodePlatforms(text string) ([]domain.Platform, error) {
	strs, err := decodeStrings(text)
	if err != nil {
		return nil, err
	}
	if len(strs) == 0 {
		return nil, nil
	}
	out := make([]domain.Platform, len(strs))
	for i, s := range strs {
		out[i] = domain.Platform(s)
	}
	return out, nil
}

func encodeShells(shells []domain.Shell) (string, error) {
	strs := make([]string, len(shells))
	for i, s := range shells {
		strs[i] = s.String()
	}
	return encodeStrings(strs)
}

func decodeShells(text string) ([]domain.Shell, error) {
	strs, err := decodeStrings(text)
	if err != nil {
		return nil, err
	}
	if len(strs) == 0 {
		return nil, nil
	}
	out := make([]domain.Shell, len(strs))
	for i, s := range strs {
		out[i] = domain.Shell(s)
	}
	return out, nil
}

// boolToInt64 and int64ToBool convert domain.Alias.Enabled to and from the
// INTEGER column sqlite (and sqlc's sqlite type inference) uses for
// boolean-shaped data.
func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func int64ToBool(v int64) bool { return v != 0 }

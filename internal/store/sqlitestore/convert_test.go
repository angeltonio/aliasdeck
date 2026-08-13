package sqlitestore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestTimeFormatIsFixedWidthAndLexicographic is a regression test for a
// bug time.RFC3339Nano has: it trims trailing-zero fractional digits, so a
// whole-second timestamp ("...05Z") and a sub-second one ("...05.5Z") are
// different widths, and "." (0x2E) sorts before "Z" (0x5A) — text
// comparison then disagrees with chronological order. query.sql's
// enrollment guard (`expires_at > ?`) and any future ORDER BY over a time
// column both rely on formatTime's output sorting the same way
// chronologically and lexicographically. This table covers ns==0 vs ns>0
// within the same second (both directions) and across a second boundary
// (both directions); it must fail if timeFormat is changed back to
// time.RFC3339Nano.
func TestTimeFormatIsFixedWidthAndLexicographic(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		earlier      time.Time
		later        time.Time
		wantEarlier  bool // want formatTime(earlier) < formatTime(later)
		wantEqualLen bool
	}{
		{
			name:         "ns==0 same second, then ns>0 later in the same second",
			earlier:      base,
			later:        base.Add(500 * time.Millisecond),
			wantEarlier:  true,
			wantEqualLen: true,
		},
		{
			name:         "ns>0 same second, then ns==0 later in the same second",
			earlier:      base.Add(1 * time.Nanosecond),
			later:        base.Add(999 * time.Millisecond),
			wantEarlier:  true,
			wantEqualLen: true,
		},
		{
			name:         "ns==0 crossing a second boundary forward",
			earlier:      base,
			later:        base.Add(1 * time.Second),
			wantEarlier:  true,
			wantEqualLen: true,
		},
		{
			name:         "ns>0 crossing a second boundary forward",
			earlier:      base.Add(500 * time.Millisecond),
			later:        base.Add(1*time.Second + 1*time.Nanosecond),
			wantEarlier:  true,
			wantEqualLen: true,
		},
		{
			name:         "ns==0 then ns==0 one second earlier (reverse order input)",
			earlier:      base.Add(-1 * time.Second),
			later:        base,
			wantEarlier:  true,
			wantEqualLen: true,
		},
		{
			name:         "ns>0 then ns>0 one second earlier, crossing back over a whole second",
			earlier:      base.Add(-1*time.Second + 1*time.Nanosecond),
			later:        base.Add(500 * time.Millisecond),
			wantEarlier:  true,
			wantEqualLen: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.earlier.Before(tc.later) {
				t.Fatalf("test fixture bug: earlier (%v) is not before later (%v)", tc.earlier, tc.later)
			}

			earlierText := formatTime(tc.earlier)
			laterText := formatTime(tc.later)

			if tc.wantEqualLen && len(earlierText) != len(laterText) {
				t.Fatalf("formatTime(%v) = %q (len %d), formatTime(%v) = %q (len %d), want equal width",
					tc.earlier, earlierText, len(earlierText), tc.later, laterText, len(laterText))
			}

			gotEarlier := earlierText < laterText
			if gotEarlier != tc.wantEarlier {
				t.Fatalf("formatTime(earlier)=%q < formatTime(later)=%q is %v, want %v — lexicographic order must match chronological order",
					earlierText, laterText, gotEarlier, tc.wantEarlier)
			}

			// Round-trip through parseTime must reproduce the original
			// instant exactly, per convert.go's timeFormat contract.
			roundTripped, err := parseTime(earlierText)
			if err != nil {
				t.Fatalf("parseTime(%q): %v", earlierText, err)
			}
			if !roundTripped.Equal(tc.earlier) {
				t.Fatalf("parseTime(formatTime(%v)) = %v, want the original instant back", tc.earlier, roundTripped)
			}
		})
	}
}

// TestReproducesExpiryComparisonBug pins the exact scenario from the
// review finding: an expired whole-second token compared against a
// half-second-later "now" must be treated as expired by TEXT comparison,
// and the mirror case (a sub-second expiry compared against a whole-second
// "now" a full second later) must be treated as still valid. Both fail
// under time.RFC3339Nano's variable-width output and both pass under
// timeFormat's fixed-width output.
func TestReproducesExpiryComparisonBug(t *testing.T) {
	expiresWholeSecond := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	nowHalfSecondLater := expiresWholeSecond.Add(500 * time.Millisecond)

	expiresText := formatTime(expiresWholeSecond)
	nowText := formatTime(nowHalfSecondLater)

	// truth: the token expired at expiresWholeSecond, and now is later, so
	// expires_at > now must be false (the guard in query.sql).
	if expiresText > nowText {
		t.Fatalf("expires_at (%q) > now (%q), want false — an expired whole-second token must not compare as still valid against a later fractional-second now", expiresText, nowText)
	}

	expiresWithFraction := time.Date(2024, 1, 1, 0, 0, 0, 500_000_000, time.UTC)
	nowWholeSecondLater := time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC)

	expiresFractionText := formatTime(expiresWithFraction)
	nowWholeText := formatTime(nowWholeSecondLater)

	// truth: the token expires at 00:00:00.5, one second before now
	// (00:00:01), so it is also expired here — expires_at > now must be
	// false too.
	if expiresFractionText > nowWholeText {
		t.Fatalf("expires_at (%q) > now (%q), want false — a sub-second expiry a full second in the past must not compare as still valid", expiresFractionText, nowWholeText)
	}
}

// TestSQLitePragmasAreApplied pins down, as a permanent regression test,
// the pragma values a review lens once (incorrectly) claimed modernc's
// pure-Go driver silently ignores. It does not: querying the live
// connection confirms all three DSN pragmas from design decision 7 are in
// effect.
func TestSQLitePragmasAreApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pragma_test.db")
	st, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()

	var foreignKeys int
	if err := st.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("PRAGMA foreign_keys = %d, want 1", foreignKeys)
	}

	var busyTimeout int
	if err := st.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("PRAGMA busy_timeout = %d, want 5000", busyTimeout)
	}

	var journalMode string
	if err := st.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("PRAGMA journal_mode = %q, want %q", journalMode, "wal")
	}
}

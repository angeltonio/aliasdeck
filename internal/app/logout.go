package app

import (
	"context"
	"fmt"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
)

// LogoutReport summarizes a `logout` run.
type LogoutReport struct {
	// SessionCleared reports whether a session was present to clear. False
	// is not an error: logging out when already logged out succeeds too.
	SessionCleared bool
}

// Logout removes the locally stored operator session and nothing else
// (design decision 17; cli-commands spec, "logout Clears the Locally Stored
// Session"). It never contacts the server — server-side revocation of a
// suspected-leaked session is a distinct, deliberate operator action (the
// server's own session-revocation route), not something this command
// performs implicitly — so it always succeeds, even when the server named
// in the credentials file is completely unreachable, because it never tries
// to reach it. The device token, if any, is left untouched: it is a
// separate credential with its own separate revocation path.
func Logout(_ context.Context, env Env, _ Options) (LogoutReport, error) {
	base, err := config.Base(env.ConfigEnv())
	if err != nil {
		return LogoutReport{}, fmt.Errorf("resolving base directory: %w", err)
	}

	credsPath := config.CredentialsFile(base)
	creds, err := config.LoadCredentials(credsPath)
	if err != nil {
		return LogoutReport{}, fmt.Errorf("loading credentials: %w", err)
	}

	cleared := creds.SessionToken != ""
	creds.SessionToken = ""
	creds.SessionExpiresAt = time.Time{}

	if creds == (config.Credentials{}) {
		// Nothing left in the credentials file at all (no session, no
		// device token): leave it exactly as-is rather than writing an
		// empty file where none existed, matching LoadCredentials' own
		// "missing file means nothing recorded yet" tolerance.
		return LogoutReport{SessionCleared: cleared}, nil
	}

	if err := config.SaveCredentials(credsPath, creds); err != nil {
		return LogoutReport{}, fmt.Errorf("saving credentials after logout: %w", err)
	}

	return LogoutReport{SessionCleared: cleared}, nil
}

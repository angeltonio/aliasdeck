package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/source"
)

// RegisterOptions configures Register.
type RegisterOptions struct {
	Options

	// URL is the server's base URL.
	URL string

	// Token is the single-use enrollment token to exchange. Mutually
	// exclusive with DeviceToken.
	Token string

	// DeviceToken adopts a device token the server has already issued —
	// what POST /api/v1/devices/{id}/token returns when an operator rotates
	// a machine's credential — instead of exchanging an enrollment token
	// for a new one.
	//
	// This exists because rotation had no landing place. The server could
	// mint a replacement credential and the operator had nowhere to put it
	// short of hand-editing credentials.json, which made rotation
	// indistinguishable from revocation in practice. The difference matters:
	// re-enrolling creates a *new* device row, so any alias pinned to the
	// old one stops reaching this machine and its history restarts.
	// Adopting keeps the machine's server-side identity and replaces only
	// the secret.
	DeviceToken string

	// Force registers again even when this device already holds a device
	// token. It is deliberately not the default: the result is a second
	// device server-side that looks identical to the first in every column
	// an operator can see, and one of them stops syncing forever.
	Force bool

	// AllowInsecureHTTP permits a non-loopback http:// URL for this
	// registration request, and — on success — is persisted into
	// config.yaml's source.allowInsecureHTTP (design decision 13), since
	// register is what actually configures this device's sync source.
	AllowInsecureHTTP bool

	// Client performs the HTTP request. Nil defaults to a client bounded at
	// serverRequestTimeout, with no retries and no redirects.
	Client *http.Client
}

// RegisterReport summarizes a successful `register` run.
type RegisterReport struct {
	DeviceID  string
	ServerURL string
}

// Register exchanges a single-use enrollment token for a device token,
// stores it separately from config.yaml at 0600 (design decision 14), and —
// only once that has succeeded — flips config.yaml's source.type to
// "server" (cli-commands spec, "register Consumes a Single-Use Enrollment
// Token"). It never requires or sends the operator password.
//
// Ordering is the whole safety property here (cli-commands spec, "Invalid
// or consumed token leaves config unchanged"): the enrollment-token exchange
// runs first and fails loudly, before either local file is touched; the
// device credential is saved second; config.yaml is written last, only once
// the device token is already safely on disk. An invalid or already-consumed
// token therefore leaves both files exactly as they were, and a failure
// saving the credentials file leaves config.yaml untouched too — the device
// token that failed to save can simply be re-registered with a fresh
// enrollment token, rather than leaving config.yaml pointed at a server
// source with no credential behind it.
//
// A failed config.yaml write is a different, narrower window (design
// decision 33, extending decision 27's accepted-partial-write precedent):
// by the time config.Write runs, the enrollment token has already been
// consumed and the device token is already safely on disk in
// credentials.json — there is no fresh enrollment token to retry with, and
// nothing to compensate by deleting (the device already exists server-side
// and its token already works). This is accepted, not compensated: the
// error below names the exact, safe manual recovery — hand-edit config.yaml's
// source: block — rather than leaving the operator to guess why sync still
// uses the old source after a "successful" registration.
func Register(ctx context.Context, env Env, opts RegisterOptions) (RegisterReport, error) {
	if opts.URL == "" {
		return RegisterReport{}, fmt.Errorf("--url is required")
	}
	switch {
	case opts.Token == "" && opts.DeviceToken == "":
		return RegisterReport{}, fmt.Errorf("--token is required (or --device-token to adopt a rotated credential)")
	case opts.Token != "" && opts.DeviceToken != "":
		return RegisterReport{}, fmt.Errorf("--token and --device-token are alternatives; pass one")
	}
	if err := source.ValidateServerURL(opts.URL, opts.AllowInsecureHTTP); err != nil {
		return RegisterReport{}, err
	}

	// loadDeviceIdentity, not loadDeviceContext: resolveSource's server arm
	// (task 8.1) requires a device token to already exist, which is exactly
	// what this function is about to obtain. Going through the full
	// deviceContext here would make a first registration attempt that
	// already flipped source.type to "server" (but whose credentials save
	// failed) permanently unable to retry, since resolveSource would refuse
	// to build a source at all without a token already on disk.
	id, err := loadDeviceIdentity(env, opts.Options)
	if err != nil {
		return RegisterReport{}, err
	}

	// Refuse to register a machine that already holds a device token, unless
	// the operator says so explicitly.
	//
	// Registering again is not idempotent: each call consumes a fresh
	// enrollment token and mints a *new* device row server-side. Doing it
	// twice on one machine leaves two devices with the same hostname,
	// platform and shell in the operator's list, identical in every visible
	// column, one of which will never sync again because this machine only
	// keeps the newest token. Found by doing exactly that during a trial and
	// then being unable to tell the two apart in the UI.
	//
	// This runs before the exchange rather than after, so a refused
	// re-registration also does not burn the enrollment token — otherwise
	// the operator would have to mint another one just to recover from a
	// command that did nothing.
	//
	// The check is for an existing *device token*, not merely a credentials
	// file. A registration whose credentials save failed leaves no token, so
	// the retry the comment above describes still works.
	existing, err := config.LoadCredentials(config.CredentialsFile(id.base))
	if err != nil {
		return RegisterReport{}, fmt.Errorf("loading existing credentials: %w", err)
	}
	if existing.DeviceToken != "" && !opts.Force && opts.DeviceToken == "" {
		// Ask the server whether the credential on disk still works before
		// refusing to replace it.
		//
		// Without this the guard fired on the mere presence of a token, which
		// made revoking a device from the control plane a dead end: the local
		// file still held the dead credential, so re-enrolling refused, and
		// the refusal's own advice ("run aliasdeck sync") could only fail with
		// the same 401. The operator had to reach for --force and read a
		// warning about abandoning a device that was already gone.
		//
		// An unreachable server is treated as "still working" on purpose.
		// Being unable to check is not evidence the credential is dead, and
		// overwriting a good one because the network was down is the worse
		// mistake of the two.
		if err := probeExistingCredential(ctx, opts, id.device, existing); err != nil {
			if !errors.Is(err, source.ErrUnauthorized) {
				// Still names the server and the device: an operator whose
				// network is down needs to know what is already here before
				// deciding whether to force past it.
				return RegisterReport{}, fmt.Errorf(
					"this device is already registered with %s as %s, and that credential could not be checked, "+
						"so nothing was changed (re-run with --force to register again anyway): %w",
					credentialServer(existing), existing.DeviceID, err)
			}
			// Refused by the server: the credential is dead, there is
			// nothing to protect, and enrolling again is exactly right.
		} else {
			where := credentialServer(existing)
			return RegisterReport{}, fmt.Errorf(
				"this device is already registered with %s as %s and that credential still works; "+
					"run `aliasdeck sync` to update it, or re-run with --force to register again "+
					"(which mints a second device server-side and abandons this one)",
				where, existing.DeviceID)
		}
	}

	if existing.DeviceToken != "" && !opts.Force && opts.DeviceToken != "" {
		where := credentialServer(existing)
		// The consequence of --force differs by mode, so the sentence
		// explaining it has to as well. Enrolling again mints a second
		// device and abandons this one; adopting a rotated credential
		// replaces only the secret and keeps this device's identity.
		// Telling an operator recovering from a leak that they are about to
		// abandon their device would talk them out of the safe path.
		// Adoption is not probed. Replacing a credential is the whole point
		// of it, so whether the current one works changes nothing about
		// whether the operator meant to do this — and an operator rotating a
		// leaked token has a reason to replace one that still works.
		return RegisterReport{}, fmt.Errorf(
			"this device is already registered with %s as %s; "+
				"re-run with --force to replace this machine's credential "+
				"(which keeps this device and changes only its token)",
			where, existing.DeviceID)
	}

	var deviceID, deviceToken string
	if opts.DeviceToken != "" {
		deviceID, deviceToken, err = adoptDeviceToken(ctx, opts, id.device)
	} else {
		deviceID, deviceToken, err = requestDeviceRegistration(ctx, opts.Client, opts.URL, opts.Token, id.device)
	}
	if err != nil {
		return RegisterReport{}, err
	}

	credsPath := config.CredentialsFile(id.base)
	creds, err := config.LoadCredentials(credsPath)
	if err != nil {
		return RegisterReport{}, fmt.Errorf("loading existing credentials: %w", err)
	}
	creds.Version = 1
	creds.ServerURL = opts.URL
	creds.DeviceID = deviceID
	creds.DeviceToken = deviceToken
	creds.ObtainedAt = env.Now()
	if err := config.SaveCredentials(credsPath, creds); err != nil {
		return RegisterReport{}, fmt.Errorf(
			"the device was registered but its credentials could not be saved locally "+
				"(the server-side device id is %q; rotate its token or contact the operator): %w",
			deviceID, err)
	}

	newCfg := id.devCfg
	newCfg.Source = config.Source{
		Type:              config.SourceTypeServer,
		URL:               opts.URL,
		AllowInsecureHTTP: opts.AllowInsecureHTTP,
	}
	if err := config.Write(id.configPath, newCfg); err != nil {
		return RegisterReport{}, fmt.Errorf(
			"the device was registered and its token was saved locally, but config.yaml could not be updated to "+
				"use it (the device token is already safe — no new enrollment token is needed; edit %s's source: "+
				"block by hand instead: set type: server, url: %q%s): %w",
			id.configPath, opts.URL, allowInsecureConfigNote(opts.AllowInsecureHTTP), err)
	}

	return RegisterReport{DeviceID: deviceID, ServerURL: opts.URL}, nil
}

// allowInsecureConfigNote names the extra source.yaml line the manual
// recovery in Register's config.Write error must mention when
// --allow-insecure was part of the original request — omitting it would
// leave a hand-edited config.yaml that ValidateServerURL rejects on the very
// next sync.
func allowInsecureConfigNote(allowInsecureHTTP bool) string {
	if !allowInsecureHTTP {
		return ""
	}
	return ", allowInsecureHTTP: true"
}

type registerWireRequest struct {
	Name          string          `json:"name"`
	Platform      domain.Platform `json:"platform"`
	Shell         domain.Shell    `json:"shell"`
	ClientVersion string          `json:"clientVersion,omitempty"`
}

type registerWireResponse struct {
	DeviceID    string `json:"deviceId"`
	DeviceToken string `json:"deviceToken"`
}

// requestDeviceRegistration performs the single, bounded
// POST /api/v1/devices/register request, mirroring internal/api/auth.go's
// registerRequest/deviceTokenResponse wire shape exactly, without importing
// internal/api (internal/app must never depend on internal/store, which
// internal/api transitively pulls in). The enrollment token is sent as a
// bearer Authorization header, matching handleDevicesRegister's own
// bearerToken extraction — it authenticates and single-use-consumes itself
// inside the handler, not via a session/device RequireKind check.
func requestDeviceRegistration(ctx context.Context, client *http.Client, baseURL, enrollmentToken string, dev domain.Device) (string, string, error) {
	reqURL, err := joinServerPath(baseURL, "api", "v1", "devices", "register")
	if err != nil {
		return "", "", err
	}

	body, err := json.Marshal(registerWireRequest{
		Name:          dev.Name,
		Platform:      dev.Platform,
		Shell:         dev.Shell,
		ClientVersion: dev.ClientVersion,
	})
	if err != nil {
		return "", "", fmt.Errorf("encoding registration request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("building registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+enrollmentToken)

	resp, err := httpClientFor(client).Do(req)
	if err != nil {
		return "", "", fmt.Errorf("registering with %s: %w", baseURL, err)
	}

	respBody, err := readLimitedBody(resp)
	if err != nil {
		return "", "", fmt.Errorf("registering with %s: %w", baseURL, err)
	}

	if resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("registration with %s failed: %s: %s", baseURL, resp.Status, serverErrorMessage(respBody))
	}

	var wire registerWireResponse
	if err := json.Unmarshal(respBody, &wire); err != nil {
		return "", "", fmt.Errorf("decoding registration response from %s: %w", baseURL, err)
	}
	return wire.DeviceID, wire.DeviceToken, nil
}

// adoptDeviceToken proves an already-issued device token works and returns
// the device identity behind it.
//
// It does that by performing a real sync through source.ServerSource rather
// than by trusting the operator's paste. Two reasons. A token that does not
// authenticate must never reach credentials.json — saving it would replace a
// working credential with a dead one and take the machine offline, which is
// the opposite of what someone recovering a leaked token wants. And the sync
// response is the only place a device is told its own id, so verifying and
// identifying are the same request rather than two.
//
// ServerSource is reused rather than reimplemented: it already validates the
// URL on every call, refuses redirects so the token cannot leak in cleartext,
// and bounds the response it will read.
func adoptDeviceToken(ctx context.Context, opts RegisterOptions, dev domain.Device) (string, string, error) {
	src := &source.ServerSource{
		URL:       opts.URL,
		Token:     opts.DeviceToken,
		Client:    opts.Client,
		AllowHTTP: opts.AllowInsecureHTTP,
	}

	resolved, err := src.Resolve(ctx, dev)
	if err != nil {
		// Deliberately does not name the cause. Resolve fails for a
		// refused token, but also for a response whose revision does not
		// match its own content — an integrity failure that has nothing to
		// do with the credential. Blaming the token there would send an
		// operator to rotate again over a problem rotating cannot fix. The
		// wrapped error says which it was.
		return "", "", fmt.Errorf(
			"could not verify that device token against %s, so nothing on this machine was changed: %w",
			opts.URL, err)
	}
	if resolved.Device.ID == "" {
		return "", "", fmt.Errorf("%s accepted the device token but did not say which device it belongs to", opts.URL)
	}
	return resolved.Device.ID, opts.DeviceToken, nil
}

// credentialServer names where an existing credential belongs, for a message
// an operator has to act on.
func credentialServer(existing config.Credentials) string {
	if existing.ServerURL == "" {
		return "a server"
	}
	return existing.ServerURL
}

// probeExistingCredential reports whether the credential already on disk is
// still accepted, returning nil when it is.
//
// It asks the server the credential claims to belong to, not the one being
// registered against: a machine moving between servers has a token that was
// never valid at the new one, and testing it there would report a refusal
// that says nothing about whether the old credential is still live.
func probeExistingCredential(ctx context.Context, opts RegisterOptions, dev domain.Device, existing config.Credentials) error {
	url := existing.ServerURL
	if url == "" {
		url = opts.URL
	}

	src := &source.ServerSource{
		URL:       url,
		Token:     existing.DeviceToken,
		Client:    opts.Client,
		AllowHTTP: opts.AllowInsecureHTTP,
	}
	_, err := src.Resolve(ctx, dev)
	return err
}

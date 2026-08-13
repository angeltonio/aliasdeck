package app

import (
	"bytes"
	"context"
	"encoding/json"
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

	// Token is the single-use enrollment token to exchange.
	Token string

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
func Register(ctx context.Context, env Env, opts RegisterOptions) (RegisterReport, error) {
	if opts.URL == "" {
		return RegisterReport{}, fmt.Errorf("--url is required")
	}
	if opts.Token == "" {
		return RegisterReport{}, fmt.Errorf("--token is required")
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

	deviceID, deviceToken, err := requestDeviceRegistration(ctx, opts.Client, opts.URL, opts.Token, id.device)
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
		return RegisterReport{}, fmt.Errorf("updating config.yaml: %w", err)
	}

	return RegisterReport{DeviceID: deviceID, ServerURL: opts.URL}, nil
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

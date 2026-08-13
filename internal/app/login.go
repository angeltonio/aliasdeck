package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/source"
)

// operatorUsername is v0.3's one fixed operator account (design decision
// 20): `login` always authenticates against it. There is no `--username`
// flag and no prompt for one — "One Operator Account" (server-auth spec)
// is what makes this name not a secret and not worth making configurable
// before that scope changes.
const operatorUsername = "admin"

// LoginOptions configures Login.
type LoginOptions struct {
	// URL is the server's base URL, e.g. "https://aliases.example.com".
	URL string

	// AllowInsecureHTTP permits a non-loopback http:// URL for this one
	// login request (`login --allow-insecure`). Login writes nothing to
	// config.yaml (design decision 17), so this has no persistent effect —
	// persisting the opt-out into config.yaml's source.allowInsecureHTTP so
	// every future sync honors it too is `register`'s job (task 8.5), since
	// register is what actually configures a device's sync source. A
	// clarification, not a contradiction, of design decision 13's own
	// wording: decision 17 (confirmed later, in Phase 1) is unambiguous that
	// login persists only a session, never config.yaml.
	AllowInsecureHTTP bool

	// PasswordStdin reads the operator password from Env.Stdin instead of
	// prompting interactively (design's Bounded Operations table, "login").
	PasswordStdin bool

	// Client performs the HTTP request. Nil defaults to a client bounded at
	// serverRequestTimeout, with no retries and no redirects.
	Client *http.Client
}

// LoginReport summarizes a successful `login` run.
type LoginReport struct {
	ServerURL string
	ExpiresAt time.Time
}

// Login authenticates the operator against a running server and stores the
// resulting session token in the credentials file — never in config.yaml
// (design decision 17; cli-commands spec, "login Authenticates the
// Operator"). It never touches config.yaml or any device token: a device's
// own sync credential, and the fact that this device even uses a server
// source, is entirely `register`'s concern.
func Login(ctx context.Context, env Env, opts LoginOptions) (LoginReport, error) {
	if opts.URL == "" {
		return LoginReport{}, fmt.Errorf("--url is required")
	}
	if err := source.ValidateServerURL(opts.URL, opts.AllowInsecureHTTP); err != nil {
		return LoginReport{}, err
	}

	password, err := resolveLoginPassword(env, opts)
	if err != nil {
		return LoginReport{}, err
	}

	base, err := config.Base(env.ConfigEnv())
	if err != nil {
		return LoginReport{}, fmt.Errorf("resolving base directory: %w", err)
	}

	token, expiresAt, err := requestLogin(ctx, opts.Client, opts.URL, password)
	if err != nil {
		return LoginReport{}, err
	}

	credsPath := config.CredentialsFile(base)
	creds, err := config.LoadCredentials(credsPath)
	if err != nil {
		return LoginReport{}, fmt.Errorf("loading existing credentials: %w", err)
	}
	creds.ServerURL = opts.URL
	creds.SessionToken = token
	creds.SessionExpiresAt = expiresAt
	if err := config.SaveCredentials(credsPath, creds); err != nil {
		return LoginReport{}, fmt.Errorf("saving session credentials: %w", err)
	}

	return LoginReport{ServerURL: opts.URL, ExpiresAt: expiresAt}, nil
}

// resolveLoginPassword acquires the operator password without ever blocking
// on a stdin that will not deliver one (design's Bounded Operations table,
// "login": "--password-stdin reads a piped stream behind the existing
// isInteractive guard; never a terminal prompt").
//
// --password-stdin is read explicitly, regardless of whether stdin looks
// like a terminal: it is the caller's own explicit request to read this way,
// mirroring `docker login --password-stdin`'s convention. Without it, this
// function consults isInteractive (internal/app/prompt.go, already
// established by `init`'s own bootstrap-confirmation prompt) before ever
// attempting to read a line: on a real terminal it prompts; on anything
// else — a closed pipe, or one that never delivers — it fails immediately,
// naming the flag that would have worked, instead of calling Scan() and
// blocking. This is the same bounded-operation shape TestRunNeverReadsStdin
// (internal/server/server_test.go) already exists to prove for `serve`'s own
// stdin, applied here to `login`'s.
func resolveLoginPassword(env Env, opts LoginOptions) (string, error) {
	if opts.PasswordStdin {
		return readLineFromStdin(env.Stdin, "stdin")
	}
	if !isInteractive(env.Stdin) {
		return "", fmt.Errorf(
			"stdin is not a terminal; rerun with --password-stdin to provide the operator password non-interactively")
	}
	fmt.Fprint(env.Stdout, "Operator password: ")
	pw, err := readLineFromStdin(env.Stdin, "stdin")
	fmt.Fprintln(env.Stdout)
	return pw, err
}

// readLineFromStdin reads a single line, trims nothing but the line
// terminator bufio.Scanner already strips, and reports a clear error rather
// than an empty string on EOF or an empty line — an accidentally-empty
// operator password must never be silently sent to the server.
func readLineFromStdin(r io.Reader, what string) (string, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("reading from %s: %w", what, err)
		}
		return "", fmt.Errorf("no password was provided on %s", what)
	}
	line := scanner.Text()
	if line == "" {
		return "", fmt.Errorf("empty password provided on %s", what)
	}
	return line, nil
}

type loginWireRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginWireResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// requestLogin performs the single, bounded POST /api/v1/auth/login request
// mirroring internal/api/auth.go's loginRequest/loginResponse wire shape
// exactly, without importing internal/api (internal/app must never depend
// on internal/store, which internal/api transitively pulls in).
func requestLogin(ctx context.Context, client *http.Client, baseURL, password string) (string, time.Time, error) {
	reqURL, err := joinServerPath(baseURL, "api", "v1", "auth", "login")
	if err != nil {
		return "", time.Time{}, err
	}

	body, err := json.Marshal(loginWireRequest{Username: operatorUsername, Password: password})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("encoding login request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("building login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClientFor(client).Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("logging in to %s: %w", baseURL, err)
	}

	respBody, err := readLimitedBody(resp)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("login to %s: %w", baseURL, err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("login to %s failed: %s: %s", baseURL, resp.Status, serverErrorMessage(respBody))
	}

	var wire loginWireResponse
	if err := json.Unmarshal(respBody, &wire); err != nil {
		return "", time.Time{}, fmt.Errorf("decoding login response from %s: %w", baseURL, err)
	}
	return wire.Token, wire.ExpiresAt, nil
}

// joinServerPath builds baseURL + the given path segments, the same
// url.JoinPath convention internal/source/server.go's syncURL already uses.
func joinServerPath(baseURL string, segments ...string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parsing %q: %w", baseURL, err)
	}
	return u.JoinPath(segments...).String(), nil
}

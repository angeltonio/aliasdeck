package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/angeltonio/aliasdeck/internal/domain"
	"github.com/angeltonio/aliasdeck/internal/validate"
)

// serverRequestTimeout bounds a single GET /api/v1/sync request end-to-end
// (design's Bounded Operations table, "ServerSource request"). There are no
// retries: a retry is a second unbounded thing, and this project has fixed
// that shape of failure enough times (internal/source/gitrun.go's
// GitTimeout) that repeating it here would be a regression, not a feature.
const serverRequestTimeout = 30 * time.Second

// ServerResponseLimit bounds how many bytes of a sync response ServerSource
// will read (design's Bounded Operations table, "Response read"). It is
// exported so a caller building a diagnostic message can reference the same
// number this package enforces.
const ServerResponseLimit = 1 << 20

// UnfilteredResolver is an additive, optional interface (design decision
// 12): a ConfigSource that can additionally return its alias set *before*
// validate.FilterValid has run over it. This is the dangerous half of that
// pair — every hostile entry Resolve would have dropped is still present in
// whatever this method returns, identically to any other unvalidated,
// server-controlled input (server-source spec, "Server Response Is Hostile
// Input"). It exists for exactly one caller: `doctor`, which runs
// validate.Config against it to explain exactly what Resolve dropped and why
// (server-source spec's success criterion 3's mechanism). Nothing this
// method returns may reach a renderer, a write path, or any output a shell
// or script will later execute — that guarantee belongs to Resolve alone.
//
// FileSource and GitSource do not implement it: `doctor` already performs
// its own independent read-and-validate pass over their aliases.yaml
// directly, and PROJECT.md §7's ConfigSource.Resolve signature is verbatim
// and shared, so this stays a second, optional interface rather than a
// widened Resolve — the same shape ResolveReporter already established
// (M3 decision 14).
type UnfilteredResolver interface {
	ResolveUnfiltered(ctx context.Context, dev domain.Device) (domain.ResolvedConfig, error)
}

// ServerSource resolves a device's aliases from a self-hosted AliasDeck
// server's GET /api/v1/sync endpoint (design decision 11; PROJECT.md §7).
//
// Its response is hostile input exactly like a local aliases.yaml or a Git
// checkout (server-source spec, "Server Response Is Hostile Input"):
// Resolve runs through the identical validate.FilterValid path FileSource
// and GitSource use, with no shortcut for the network origin — a
// compromised server must never gain a capability a malicious local file
// does not have.
//
// Its methods have pointer receivers: a successful Resolve records what it
// did (design decision 11's FetchedAt/Stale) so a later LastResolve call can
// report it, mirroring *GitSource. Callers must therefore use *ServerSource,
// not a ServerSource value, wherever it is stored as a ConfigSource.
type ServerSource struct {
	// URL is the server's base URL, e.g. "https://aliases.example.com".
	// Checked by ValidateServerURL on every Resolve call (design decision
	// 13) — not only once at login — so hand-editing config.yaml to
	// http:// cannot quietly downgrade a device enrolled securely.
	URL string
	// Token is the device's bearer token (server-source spec, "Device Token
	// Stored Outside config.yaml"). It is sent only as the Authorization
	// header and never appears in an error message this package produces.
	Token string
	// Client performs the HTTP request. Nil defaults to a client bounded at
	// serverRequestTimeout with no retry behavior configured.
	Client *http.Client
	// AllowHTTP permits a non-loopback http:// URL (login --allow-insecure;
	// design decision 13). It has no effect on an https:// URL or a
	// loopback http:// URL, both of which are always allowed regardless.
	AllowHTTP bool

	// Now returns the current time; nil defaults to time.Now. Only unit
	// tests override it, to make LastResolve's FetchedAt deterministic.
	Now func() time.Time

	last ResolveInfo
}

// Descriptor identifies this source for `status`.
func (s *ServerSource) Descriptor() Descriptor {
	return Descriptor{Type: "server", Ref: s.URL}
}

// LastResolve implements ResolveReporter. Stale is always false (design
// decision 11): a stale response can never exist because ServerSource keeps
// no cache — an unreachable server is a hard error, not a fallback.
func (s *ServerSource) LastResolve() ResolveInfo { return s.last }

// Resolve implements ConfigSource. It fetches the device's sync response,
// verifies its revision, and filters the result through validate.FilterValid
// before returning it — the same defense FileSource and GitSource apply to
// every other origin (server-source spec, "Server Response Is Hostile
// Input").
func (s *ServerSource) Resolve(ctx context.Context, dev domain.Device) (domain.ResolvedConfig, error) {
	resolved, err := s.resolveUnfiltered(ctx, dev)
	if err != nil {
		return domain.ResolvedConfig{}, err
	}

	filtered, _ := validate.FilterValid(resolved)
	return filtered, nil
}

// ResolveUnfiltered implements UnfilteredResolver (design decision 12). It
// performs the same fetch and revision check Resolve does, but returns the
// alias set exactly as the server sent it — unvalidated: every hostile entry
// validate.FilterValid would otherwise drop is still present. This is
// hostile input, exactly like Resolve's own input before filtering; the only
// difference is that nothing here has removed the hostile entries yet.
//
// This exists for exactly one caller: `doctor`, which runs validate.Config
// against the result to explain what Resolve would have dropped and why.
// The result of this call MUST NEVER reach a renderer, a write path, or any
// output a shell will later execute — only Resolve's filtered result may.
// Reach for Resolve, not this method, unless you are writing diagnostics.
//
// This makes one HTTP request, the same as Resolve — not a second one on
// top of it.
func (s *ServerSource) ResolveUnfiltered(ctx context.Context, dev domain.Device) (domain.ResolvedConfig, error) {
	return s.resolveUnfiltered(ctx, dev)
}

// resolveUnfiltered is Resolve and ResolveUnfiltered's shared implementation.
func (s *ServerSource) resolveUnfiltered(ctx context.Context, dev domain.Device) (domain.ResolvedConfig, error) {
	if err := ValidateServerURL(s.URL, s.AllowHTTP); err != nil {
		return domain.ResolvedConfig{}, fmt.Errorf("server source: %w", err)
	}

	wire, err := s.fetchSync(ctx, dev)
	if err != nil {
		return domain.ResolvedConfig{}, err
	}

	resolved, err := wire.toResolvedConfig()
	if err != nil {
		return domain.ResolvedConfig{}, fmt.Errorf("server source %s: %w", s.URL, err)
	}

	s.last = ResolveInfo{
		FetchedAt: s.now(),
		Stale:     false,
	}

	return resolved, nil
}

// fetchSync performs the single, bounded GET /api/v1/sync request and
// decodes its body. There are no retries anywhere in this path.
func (s *ServerSource) fetchSync(ctx context.Context, dev domain.Device) (serverSyncResponse, error) {
	reqURL, err := s.syncURL(dev)
	if err != nil {
		return serverSyncResponse{}, fmt.Errorf("server source %s: %w", s.URL, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return serverSyncResponse{}, fmt.Errorf("server source %s: building request: %w", s.URL, err)
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient().Do(req)
	if err != nil {
		if errors.Is(err, errRedirectRefused) {
			return serverSyncResponse{}, fmt.Errorf("server source %s: %w", s.URL, err)
		}
		return serverSyncResponse{}, fmt.Errorf("server source %s: unreachable: %w", s.URL, err)
	}
	defer resp.Body.Close()

	// Bounded read (design's Bounded Operations table, "Response read"): at
	// most ServerResponseLimit+1 bytes, so an oversized response is
	// detected explicitly rather than by an incidental JSON parse failure,
	// and this process never buffers an unbounded amount of attacker- or
	// operator-controlled data.
	body, err := io.ReadAll(io.LimitReader(resp.Body, ServerResponseLimit+1))
	if err != nil {
		return serverSyncResponse{}, fmt.Errorf("server source %s: reading response: %w", s.URL, err)
	}
	if len(body) > ServerResponseLimit {
		return serverSyncResponse{}, fmt.Errorf(
			"server source %s: response exceeds the %d byte limit", s.URL, ServerResponseLimit)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return serverSyncResponse{}, fmt.Errorf(
			"server source %s: %s: %s", s.URL, resp.Status, serverErrorMessage(body))
	}

	var wire serverSyncResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return serverSyncResponse{}, fmt.Errorf("server source %s: decoding sync response: %w", s.URL, err)
	}
	return wire, nil
}

// syncURL builds the GET /api/v1/sync?platform=&shell= URL (design decision
// 9's exact shape).
func (s *ServerSource) syncURL(dev domain.Device) (string, error) {
	u, err := url.Parse(s.URL)
	if err != nil {
		return "", fmt.Errorf("parsing server url: %w", err)
	}
	u = u.JoinPath("api", "v1", "sync")

	q := u.Query()
	q.Set("platform", dev.Platform.String())
	q.Set("shell", dev.Shell.String())
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// errRedirectRefused is checked with errors.Is in fetchSync so a refused
// redirect is reported plainly rather than folded into the generic
// "unreachable" wording every other transport error gets there.
var errRedirectRefused = errors.New("refusing to follow redirect")

// refuseRedirect is installed as CheckRedirect on every client fetchSync
// uses (design decision 13's correction, bounded-review CRITICAL 1). Go's
// default redirect handling forwards the Authorization header whenever the
// redirect target's canonical host:port matches the original request's —
// a comparison that never looks at scheme — so an https:// sync endpoint
// that answers with a same-host http:// redirect kept re-sending this
// device's bearer token in cleartext, achieving in one 302 the exact
// downgrade design decision 13 already exists to stop a hand-edited
// config.yaml from causing. ValidateServerURL cannot see this: it only
// inspects the configured base URL before the request leaves, never a
// Location header the server returns afterward.
//
// The sync endpoint has no legitimate reason to redirect at all — a reverse
// proxy fronting the API should serve the route directly, not bounce it —
// so every redirect is refused outright rather than special-cased by scheme
// or host. This matches this project's existing bounded-operations posture
// (design's Bounded Operations table, "ServerSource request": "No retries —
// a retry is a second unbounded thing"): no retries, no redirects, fail
// with a clear message naming what happened instead of quietly doing
// something clever.
func refuseRedirect(req *http.Request, via []*http.Request) error {
	return fmt.Errorf(
		"%w: the sync endpoint redirected to %s; the sync endpoint must not redirect — point source.url "+
			"directly at the API, not at a proxy or rule that redirects it",
		errRedirectRefused, req.URL)
}

// httpClient returns a client bounded at Client's (or, when Client is nil,
// serverRequestTimeout's) timeout, that never follows a redirect.
//
// A caller-supplied Client's own CheckRedirect is intentionally never
// consulted or overwritten in place: this always constructs a fresh
// *http.Client wrapping Client's Transport (or the default one), so a
// *http.Client value the caller owns and may use elsewhere is never mutated,
// while every request this package ever sends is still bound by
// refuseRedirect regardless of whether Client was provided.
func (s *ServerSource) httpClient() *http.Client {
	timeout := serverRequestTimeout
	var transport http.RoundTripper
	var jar http.CookieJar
	if s.Client != nil {
		timeout = s.Client.Timeout
		transport = s.Client.Transport
		jar = s.Client.Jar
	}
	return &http.Client{
		Transport:     transport,
		Jar:           jar,
		Timeout:       timeout,
		CheckRedirect: refuseRedirect,
	}
}

func (s *ServerSource) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// serverSyncResponse mirrors internal/api/sync.go's syncResponse wire shape
// exactly — {revision, device{id,name,platform,shell,profileIds},
// aliases[{name,command,description}], generatedAt} — without importing
// internal/api (internal/source must never depend on internal/store,
// transitively pulled in through internal/api's own auth/store imports).
// This is a shared wire contract, not a package dependency.
type serverSyncResponse struct {
	Revision string            `json:"revision"`
	Device   serverSyncDevice  `json:"device"`
	Aliases  []serverSyncAlias `json:"aliases"`
}

type serverSyncDevice struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Platform   string   `json:"platform"`
	Shell      string   `json:"shell"`
	ProfileIDs []string `json:"profileIds"`
}

type serverSyncAlias struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Description string `json:"description"`
}

// toResolvedConfig converts the wire response into a domain.ResolvedConfig
// and hard-fails on a revision mismatch (server-source spec via design's
// technical approach: "the client can verify the server's revision
// byte-for-byte and hard-fail on a mismatch").
//
// domain.Resolve runs again here, client-side, over the already-filtered
// wire set: every alias arrives Enabled with no targeting fields, which
// makes this call an identity operation whose only job is recomputing the
// revision the same way the server did (ComputeRevision hashes exactly
// platform, shell, and per-alias name/command/description — precisely the
// fields the wire carries).
func (w serverSyncResponse) toResolvedConfig() (domain.ResolvedConfig, error) {
	platform, err := domain.ParsePlatform(w.Device.Platform)
	if err != nil {
		return domain.ResolvedConfig{}, fmt.Errorf("sync response: %w", err)
	}
	shell, err := domain.ParseShell(w.Device.Shell)
	if err != nil {
		return domain.ResolvedConfig{}, fmt.Errorf("sync response: %w", err)
	}

	dev := domain.Device{
		ID:         w.Device.ID,
		Name:       w.Device.Name,
		Platform:   platform,
		Shell:      shell,
		ProfileIDs: w.Device.ProfileIDs,
	}

	aliases := make([]domain.Alias, 0, len(w.Aliases))
	for _, a := range w.Aliases {
		aliases = append(aliases, domain.Alias{
			Name:        a.Name,
			Command:     a.Command,
			Description: a.Description,
			Enabled:     true,
		})
	}

	resolved := domain.Resolve(dev, aliases)
	if resolved.Revision != w.Revision {
		return domain.ResolvedConfig{}, fmt.Errorf(
			"sync response revision mismatch: server reported %q, recomputed %q", w.Revision, resolved.Revision)
	}

	return resolved, nil
}

// serverErrorMessage extracts a human-readable message from a non-2xx
// response body shaped like internal/api's own error responses
// ({"error":{"message":"..."}}), falling back to a fixed, generic message
// when the body does not parse as that shape — an internal server's raw
// body text must never be surfaced verbatim to a user.
func serverErrorMessage(body []byte) string {
	var wire struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &wire); err == nil && wire.Error.Message != "" {
		return wire.Error.Message
	}
	return "request failed"
}

package source

import (
	"context"
	"encoding/json"
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
// 12): a ConfigSource that can report the resolved configuration *before*
// validate.FilterValid runs, so `doctor` can explain exactly what would be
// dropped and why (server-source spec's success criterion 3's mechanism).
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

// ResolveUnfiltered implements UnfilteredResolver (design decision 12): the
// same fetch and revision check Resolve performs, without the final
// validate.FilterValid step, so `doctor` can run validate.Config against
// the unfiltered set to explain exactly what Resolve would have dropped.
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

// httpClient returns Client, or a client bounded at serverRequestTimeout
// when Client is nil.
func (s *ServerSource) httpClient() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: serverRequestTimeout}
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

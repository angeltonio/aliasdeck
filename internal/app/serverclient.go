package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// serverRequestTimeout bounds a single `login`/`register` HTTP request
// end-to-end, mirroring internal/source/server.go's identical
// serverRequestTimeout (design's Bounded Operations table, "ServerSource
// request" — the same posture applies to every direct server request this
// package makes, not only the sync one). There are no retries here either:
// a retry is a second unbounded thing, and this project has already fixed
// that shape of failure enough times (internal/source/gitrun.go's
// GitTimeout) that repeating it would be a regression.
//
// internal/app cannot import internal/source's unexported constant, so this
// is a second declaration of the same value rather than a shared one —
// deliberately, the same trade design decision 16 already accepted for
// serverValidationShells rather than importing across a boundary for one
// constant.
const serverRequestTimeout = 30 * time.Second

// serverResponseLimit bounds how many bytes of a login/register response
// this package will read (design's Bounded Operations table, "Response
// read"), mirroring internal/source.ServerResponseLimit — duplicated for the
// same reason serverRequestTimeout is above.
const serverResponseLimit = 1 << 20

// httpClientFor returns client bounded at serverRequestTimeout (when client
// is nil) and configured to refuse every HTTP redirect, mirroring
// internal/source/server.go's own httpClient()/refuseRedirect: Go's default
// redirect handling forwards the Authorization header whenever the redirect
// target's canonical host:port matches the original request's, regardless
// of scheme, which would re-send an operator password or an enrollment
// token in cleartext on a same-host https->http redirect (design decision
// 31's exact hazard, here for login/register's own requests rather than
// sync's). Neither `login` nor `register` has any legitimate reason to be
// redirected, so every redirect is refused outright rather than special-
// cased by scheme.
//
// A caller-supplied client's own Transport/Timeout are preserved into a
// freshly constructed *http.Client; the caller's value is never mutated in
// place.
func httpClientFor(client *http.Client) *http.Client {
	timeout := serverRequestTimeout
	var transport http.RoundTripper
	if client != nil {
		timeout = client.Timeout
		transport = client.Transport
	}
	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: refuseServerRedirect,
	}
}

var errServerRedirectRefused = errors.New("refusing to follow redirect")

func refuseServerRedirect(req *http.Request, _ []*http.Request) error {
	return fmt.Errorf("%w: %s redirected to %s; point --url directly at the API, not at a proxy or rule that redirects it",
		errServerRedirectRefused, req.URL, req.URL)
}

// readLimitedBody reads at most serverResponseLimit+1 bytes of resp.Body, so
// an oversized response is detected explicitly (design's Bounded Operations
// table, "Response read") rather than by an incidental JSON parse failure.
func readLimitedBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, serverResponseLimit+1))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if len(body) > serverResponseLimit {
		return nil, fmt.Errorf("response exceeds the %d byte limit", serverResponseLimit)
	}
	return body, nil
}

// serverErrorMessage extracts a human-readable message from a non-2xx
// response body shaped like internal/api's own error responses
// ({"error":{"message":"..."}}), falling back to a fixed, generic message
// when the body does not parse as that shape. Mirrors
// internal/source/server.go's function of the same name and purpose — an
// internal server's raw body text must never be surfaced verbatim to a
// user, from this package's own direct requests either.
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

package source

import (
	"fmt"
	"net"
	"net/url"
)

// ValidateServerURL enforces the transport guard design decision 13
// requires: https:// is always accepted; http:// is accepted only when the
// host is loopback (127.0.0.0/8, ::1, "localhost") or allowHTTP is true
// (config.yaml's source.allowInsecureHTTP, set only by `login
// --allow-insecure`). Anything else — an unparseable URL, or a scheme that
// is neither http nor https — is rejected outright.
//
// server-source spec, "HTTPS Required Unless Loopback or Explicit Opt-Out".
// This is called both at `login` and on every `sync` (design decision 13):
// checking it only once, at enrollment, would let a hand-edited config.yaml
// quietly downgrade a device that was enrolled securely.
func ValidateServerURL(raw string, allowHTTP bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("server url %q is not a valid URL: %w", raw, err)
	}

	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		if allowHTTP {
			return nil
		}
		return fmt.Errorf(
			"server url %q is not https and is not loopback; pass the explicit insecure opt-out (login --allow-insecure) to use it anyway",
			raw)
	default:
		return fmt.Errorf(
			"server url %q must use https:// (or http:// for a loopback address or with the insecure opt-out), got scheme %q",
			raw, u.Scheme)
	}
}

// isLoopbackHost reports whether host — a URL's already-extracted hostname,
// with no port and no IPv6 brackets — refers to the local machine: the
// literal name "localhost", or an IP literal inside 127.0.0.0/8 or ::1.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

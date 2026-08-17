package server

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const publicURLEnv = "ALIASDECK_PUBLIC_URL"

func resolvePublicURL(cfg Config) (*url.URL, error) {
	raw := cfg.PublicURL
	if raw == "" {
		raw = cfg.Getenv(publicURLEnv)
	}
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Opaque != "" || u.Host == "" {
		return nil, fmt.Errorf("server: %s must be an absolute origin URL", publicURLEnv)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.EscapedPath() != "" && u.EscapedPath() != "/") {
		return nil, fmt.Errorf("server: %s must contain only scheme and authority (no credentials, path, query, or fragment)", publicURLEnv)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("server: %s scheme must be http or https", publicURLEnv)
	}
	hostname := u.Hostname()
	if hostname == "" || strings.ContainsAny(hostname, "\x00\r\n\t ") {
		return nil, fmt.Errorf("server: %s has an invalid host", publicURLEnv)
	}
	if port := u.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return nil, fmt.Errorf("server: %s has an invalid port", publicURLEnv)
		}
	}
	if u.Scheme == "http" && !isLoopbackPublicHost(hostname) {
		return nil, fmt.Errorf("server: %s requires https for non-loopback hosts", publicURLEnv)
	}
	u.Path, u.RawPath = "", ""
	return u, nil
}

func isLoopbackPublicHost(host string) bool {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Package web serves AliasDeck's operator-facing HTML interface.
//
// Browser sessions reuse the same revocable token store as the JSON API.
// Every authenticated mutation requires an HMAC-derived per-session CSRF
// token, password verification shares one process-wide concurrency limiter
// with the API, and reverse-proxy deployments use an explicitly configured
// public origin rather than trusting forwarding headers.
//
// The package renders directly against internal/store and never calls the
// JSON API over HTTP from inside the process. It must not import renderers:
// the server transmits structured data and the client produces shell syntax.
package web

// Package auth mints and verifies operator sessions, device tokens and the
// one-time operator bootstrap credential. It MUST NOT import
// internal/renderers — the server transmits data, the client produces shell
// syntax (design decision 2).
package auth

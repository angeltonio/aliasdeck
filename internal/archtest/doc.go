// Package archtest makes AliasDeck's central boundary mechanical instead of
// cultural: the server transmits data, the client produces shell syntax
// (design decision 2, docs/PROJECT.md §3.7/§6.1). deps_test.go shells out to
// `go list -deps` and fails the build the first time a server package
// reaches for internal/renderers, or a client package reaches for
// internal/store / modernc.org/sqlite.
package archtest

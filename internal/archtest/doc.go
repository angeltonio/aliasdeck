// Package archtest makes AliasDeck's central boundaries mechanical instead
// of cultural: the server transmits data, the client produces shell syntax
// (design decision 2, docs/PROJECT.md §3.7/§6.1), and the client and server
// ship as two separate binaries (design decision reversing the earlier
// single-binary model, docs/WHAT-WE-ARE-BUILDING.md). deps_test.go shells
// out to `go list -deps` and fails the build the first time a server
// package reaches for internal/renderers, a client package reaches for
// internal/store / modernc.org/sqlite, or cmd/aliasdeck itself reaches for
// internal/store, internal/api, internal/server, internal/sync, or
// modernc.org/sqlite.
package archtest

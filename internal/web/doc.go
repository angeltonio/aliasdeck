// Package web is a PROTOTYPE, not milestone work.
//
// Its only purpose is to let the project owner open a browser and judge
// whether "Go html/template + htmx, served from the same binary" is a
// direction worth carrying into Milestone 5 (openspec/changes/web-ui), or
// whether the Vite + React + Tailwind + shadcn/ui stack docs/PROJECT.md
// currently specifies should stand. It is deliberately narrow:
//
//   - Login (cookie session), alias list/create/delete, device list, and
//     the "copy the commands" add-device flow only.
//   - No profile screens, no device rename/revoke/rotate, no first-run
//     setup, no owner_id — all of that is real milestone work this
//     prototype does not attempt.
//   - No CSRF token. The session cookie is HttpOnly + SameSite=Strict,
//     which blocks the common cross-site form-post case, but there is no
//     per-session double-submit token on state-changing forms the way the
//     milestone proposal calls for. A production build of this feature
//     MUST add one before the cookie transport is trusted.
//   - Untested by this project's own standards: no unit or integration
//     tests ship with this package. It was verified once, by hand, over
//     real HTTP against an ephemeral httptest/loopback listener — see the
//     apply report — and nothing more. Do not merge this package as
//     production code without the tests every other internal/ package in
//     this repository carries.
//
// It renders directly against internal/store (the same seam internal/api
// uses) and never calls the JSON API over HTTP from inside its own
// process. It must never import internal/renderers — the server transmits
// data, the client produces shell syntax (design decision 2) — and
// internal/archtest's TestServerPackagesNeverImportRenderers already
// enforces that transitively, because internal/server imports this
// package.
package web

package api

import (
	_ "embed"
	"net/http"
)

// openapiPattern is where the embedded spec is served (design decision 15).
const openapiPattern = "/api/v1/openapi.yaml"

// embeddedOpenAPISpec is this file's own copy of the checked-in contract.
// go:embed cannot reference a path outside its package directory (the same
// constraint design decision 5 already hit for internal/store/migrations,
// which is why root migrations/ moved under internal/store/migrations
// instead), so the canonical, human-browsed docs/openapi.yaml cannot be
// embedded directly from here. openapi_coverage_test.go's
// TestEmbeddedOpenAPISpecMatchesDocsCopy keeps this file and docs/openapi.yaml
// byte-identical the same way CI's sqlc-generate check (design decision 6)
// keeps a different checked-in artifact honest: a diff between them fails
// the test, naming exactly what drifted.
//
//go:embed openapi.yaml
var embeddedOpenAPISpec []byte

// handleOpenAPISpec serves the checked-in OpenAPI document verbatim. It is
// Public: the document describes only shapes and requirements already true
// of a public, checked-in file (docs/PROJECT.md, this repository), and
// gating documentation behind a session token would only make the contract
// harder for an operator to find before they have one.
func (a *api) handleOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(embeddedOpenAPISpec)
}

package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// openapiDoc is the minimal shape this test needs out of docs/openapi.yaml
// — just enough to enumerate every documented "METHOD path" pair, not a
// full OpenAPI schema model.
type openapiDoc struct {
	Paths map[string]map[string]any `yaml:"paths"`
}

// routeSet maps a URL pattern to the set of HTTP methods registered (or
// documented) for it.
type routeSet map[string]map[string]bool

func (rs routeSet) add(pattern, method string) {
	if rs[pattern] == nil {
		rs[pattern] = map[string]bool{}
	}
	rs[pattern][strings.ToUpper(method)] = true
}

// registeredRoutes returns every "METHOD pattern" the production router
// actually registers — (*api).routes(), the exact slice NewRouter builds
// from. A zero-value *api is sufficient: routes() only takes method values
// on a itself to form closures, it never calls the store while building
// the slice.
func registeredRoutes(t *testing.T) routeSet {
	t.Helper()
	a := &api{}
	out := routeSet{}
	for _, r := range a.routes() {
		out.add(r.Pattern, r.Method)
	}
	return out
}

// httpMethodKeys is every key OpenAPI 3.0 allows directly under a path
// item that names an actual HTTP method — as opposed to "parameters",
// "summary", "description" or any other path-item-level key that is not a
// route by itself.
var httpMethodKeys = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true,
	"delete": true, "head": true, "options": true, "trace": true,
}

// documentedRoutes parses the embedded OpenAPI spec (the same bytes
// handleOpenAPISpec serves) into a routeSet.
func documentedRoutes(t *testing.T) routeSet {
	t.Helper()
	var doc openapiDoc
	if err := yaml.Unmarshal(embeddedOpenAPISpec, &doc); err != nil {
		t.Fatalf("parsing embedded openapi.yaml: %v", err)
	}
	out := routeSet{}
	for path, methods := range doc.Paths {
		for method := range methods {
			if !httpMethodKeys[strings.ToLower(method)] {
				continue
			}
			out.add(path, method)
		}
	}
	return out
}

// TestOpenAPIDocumentsExactlyTheRegisteredRoutes is design decision 15's
// coverage mechanism (task 5.13): a bidirectional 1:1 match between the
// router's own route table and the embedded OpenAPI document. Both
// directions are checked in one test, by construction, so neither a route
// added without documentation nor a documented route that no longer exists
// can pass silently:
//
//   - A route present in registeredRoutes() but absent from
//     documentedRoutes() fails the first loop, naming the undocumented
//     route.
//   - A route present in documentedRoutes() but absent from
//     registeredRoutes() fails the second loop, naming the stale
//     documentation entry.
func TestOpenAPIDocumentsExactlyTheRegisteredRoutes(t *testing.T) {
	registered := registeredRoutes(t)
	documented := documentedRoutes(t)

	for pattern, methods := range registered {
		for method := range methods {
			if !documented[pattern][method] {
				t.Errorf("route %s %s is registered but not documented in docs/openapi.yaml", method, pattern)
			}
		}
	}

	for pattern, methods := range documented {
		for method := range methods {
			if !registered[pattern][method] {
				t.Errorf("docs/openapi.yaml documents %s %s but no such route is registered", method, pattern)
			}
		}
	}
}

// TestEmbeddedOpenAPISpecMatchesDocsCopy keeps internal/api/openapi.yaml
// (what go:embed can actually reach — it cannot escape its own package
// directory, the same constraint that moved migrations under
// internal/store/migrations) byte-identical to the human-browsed
// docs/openapi.yaml. A future edit to only one of the two copies fails
// here instead of silently shipping a served spec that disagrees with the
// one anybody reading the repository sees.
func TestEmbeddedOpenAPISpecMatchesDocsCopy(t *testing.T) {
	docsCopy, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("reading docs/openapi.yaml: %v", err)
	}
	if string(docsCopy) != string(embeddedOpenAPISpec) {
		t.Fatal("internal/api/openapi.yaml (embedded) and docs/openapi.yaml have drifted apart — keep them byte-identical")
	}
}

// TestHandleOpenAPISpecServesTheEmbeddedDocumentPublicly proves
// openapiPattern is actually wired, unauthenticated, to the embedded
// bytes — not merely present in routes()'s table.
func TestHandleOpenAPISpecServesTheEmbeddedDocumentPublicly(t *testing.T) {
	h := newTestRouter(t, newFakeStore())

	rec := doRequest(h, "GET", openapiPattern, "", nil)
	if rec.Code != 200 {
		t.Fatalf("GET %s without auth = %d, want 200", openapiPattern, rec.Code)
	}
	if rec.Body.String() != string(embeddedOpenAPISpec) {
		t.Fatal("GET openapiPattern did not return the embedded spec verbatim")
	}
}

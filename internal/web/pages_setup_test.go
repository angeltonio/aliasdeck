package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/store/sqlitestore"
)

func TestSetupRouteRequiresCredentialAndCreatesOperator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-credential")
	if err := auth.EnsureSetupCredential(path, nil); err != nil {
		t.Fatal(err)
	}
	credential, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlitestore.Open(context.Background(), filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a := &webapp{store: st, now: time.Now, setupCredentialPath: path}
	a.tmpl, err = loadTemplates()
	if err != nil {
		t.Fatal(err)
	}

	noCredential := httptest.NewRecorder()
	a.handleSetupPage(noCredential, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if noCredential.Code != http.StatusNotFound {
		t.Fatalf("uncredentialed setup status = %d, want 404", noCredential.Code)
	}

	form := url.Values{"credential": {strings.TrimSpace(string(credential))}, "username": {"operator"}, "password": {"correct horse battery staple"}, "confirmation": {"correct horse battery staple"}}
	rec := httptest.NewRecorder()
	setupRequest := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	setupRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a.handleSetupSubmit(rec, setupRequest)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("setup status = %d, want redirect", rec.Code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("setup credential remains after success: %v", err)
	}

	replay := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	replayRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a.handleSetupSubmit(replay, replayRequest)
	if replay.Code != http.StatusNotFound {
		t.Fatalf("replay status = %d, want 404", replay.Code)
	}
}

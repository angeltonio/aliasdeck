package web

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
)

func TestCatalogsHaveExactParity(t *testing.T) {
	keys := func(catalog map[string]string) []string {
		out := make([]string, 0, len(catalog))
		for key := range catalog {
			out = append(out, key)
		}
		sort.Strings(out)
		return out
	}
	en, es := keys(messages[languageEnglish]), keys(messages[languageSpanish])
	if strings.Join(en, "\n") != strings.Join(es, "\n") {
		t.Fatalf("English and Spanish catalog keys differ\nEnglish: %v\nSpanish: %v", en, es)
	}
	for lang, catalog := range messages {
		for key, value := range catalog {
			if value == "" {
				t.Errorf("catalog %s has empty value for %q", lang, key)
			}
		}
	}
}

func TestTranslationFallsBackSafely(t *testing.T) {
	if got := translate(language("fr"), "login.title"); got != "Log in" {
		t.Fatalf("unsupported language fallback = %q", got)
	}
	if got := translate(languageSpanish, "missing.key"); got != "missing.key" {
		t.Fatalf("missing key fallback = %q", got)
	}
}

func TestRequestLanguageNegotiationPrecedenceAndRegionalTags(t *testing.T) {
	tests := []struct {
		name, target, cookie, accept string
		want                         language
	}{
		{"explicit beats cookie and browser", "/login?lang=es", "en", "en-US", languageSpanish},
		{"cookie beats browser", "/login", "es", "en-US", languageSpanish},
		{"regional Spanish", "/login", "", "es-ES,es;q=0.9,en;q=0.8", languageSpanish},
		{"weighted English", "/login", "", "es-MX;q=0.5,en-GB;q=0.9", languageEnglish},
		{"invalid explicit falls through", "/login?lang=de", "es", "en", languageSpanish},
		{"unsupported defaults English", "/login", "", "fr-FR", languageEnglish},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: languageCookieName, Value: tt.cookie})
			}
			req.Header.Set("Accept-Language", tt.accept)
			if got := requestLanguage(req); got != tt.want {
				t.Fatalf("requestLanguage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLanguageSelectionCookieAndSafeReturn(t *testing.T) {
	a, _, _ := newDeviceStatusTestApp(t)
	form := url.Values{"language": {"es"}, "return": {"/devices?view=all"}}
	req := httptest.NewRequest(http.MethodPost, "/language", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	a.handleLanguage(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/devices?view=all" {
		t.Fatalf("language redirect = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != languageCookieName || cookie.Value != "es" || cookie.Path != "/" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge <= 0 {
		t.Fatalf("language cookie attributes = %#v", cookie)
	}

	for _, target := range []string{"https://evil.example/", "//evil.example/", `/\\evil.example/`} {
		bad := url.Values{"language": {"es"}, "return": {target}}
		req := httptest.NewRequest(http.MethodPost, "/language", strings.NewReader(bad.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		a.handleLanguage(rec, req)
		if got := rec.Header().Get("Location"); got != "/" {
			t.Errorf("unsafe return %q redirected to %q", target, got)
		}
	}
}

func TestSelectorReturnTargetPreservesPageAndDropsLanguageOverride(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/setup?credential=opaque&lang=es", nil)
	if got := returnTargetFor(req); got != "/setup?credential=opaque" {
		t.Fatalf("returnTargetFor() = %q", got)
	}
}

func TestLanguageSelectionRejectsInvalidValue(t *testing.T) {
	a, _, _ := newDeviceStatusTestApp(t)
	req := httptest.NewRequest(http.MethodPost, "/language", strings.NewReader("language=es-ES&return=%2Fdevices"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.handleLanguage(rec, req)
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("invalid selection = %d cookie %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
}

func TestExplicitQueryLanguageIsPersistedOnWebPages(t *testing.T) {
	a, _, _ := newDeviceStatusTestApp(t)
	handler, err := newMux(a)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/login?lang=es-ES", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `<html lang="es">`) {
		t.Fatalf("explicit regional language did not localize page: %q", rec.Body.String())
	}
	var found bool
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == languageCookieName && cookie.Value == "es" {
			found = true
		}
	}
	if !found {
		t.Fatal("explicit query language was not persisted")
	}
}

func TestCorePagesAndFragmentsRenderSpanish(t *testing.T) {
	a, _, _ := newDeviceStatusTestApp(t)
	cases := []struct {
		name   string
		render func(http.ResponseWriter, *http.Request)
		want   []string
	}{
		{"login", a.handleLoginPage, []string{`<html lang="es">`, "Iniciar sesión", `action="/language"`}},
		{"devices", a.handleDevicesPage, []string{`<html lang="es">`, "Dispositivos", "Las marcas de tiempo se muestran en UTC."}},
		{"add device", a.handleDevicesAddPage, []string{`<html lang="es">`, "Añadir un dispositivo", "Sincronizar alias automáticamente", "Descarga los cambios de alias en segundo plano y mantiene actualizado el estado de conexión del dispositivo en macOS. En otras plataformas, la configuración se completa sin inicio automático.", "Frecuencia de sincronización de alias", ">5 segundos</option>"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Accept-Language", "es-ES")
			rec := httptest.NewRecorder()
			tt.render(rec, req)
			body := rec.Body.String()
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("body missing %q: %q", want, body)
				}
			}
		})
	}

	req := httptest.NewRequest(http.MethodPost, "/aliases", nil)
	req.Header.Set("Accept-Language", "es")
	rec := httptest.NewRecorder()
	a.respondAliasPanel(req, rec, http.StatusOK, "")
	if body := rec.Body.String(); !strings.Contains(body, "Aún no hay alias") || strings.Contains(body, "No aliases yet") {
		t.Fatalf("Spanish HTMX fragment = %q", body)
	}
}

func TestSpanishValidationAndEnrollmentFragments(t *testing.T) {
	a, _, _ := newDeviceStatusTestApp(t)

	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=missing&password=wrong"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login.AddCookie(&http.Cookie{Name: languageCookieName, Value: "es"})
	loginRec := httptest.NewRecorder()
	a.handleLoginSubmit(loginRec, login)
	if !strings.Contains(loginRec.Body.String(), "el nombre de usuario o la contraseña no son válidos") {
		t.Fatalf("Spanish login validation = %q", loginRec.Body.String())
	}

	alias := httptest.NewRequest(http.MethodPost, "/aliases", strings.NewReader("name=x&command="))
	alias.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	alias.AddCookie(&http.Cookie{Name: languageCookieName, Value: "es"})
	aliasRec := httptest.NewRecorder()
	a.handleAliasesCreate(aliasRec, alias)
	if !strings.Contains(aliasRec.Body.String(), "el comando está vacío") || !strings.Contains(aliasRec.Body.String(), "Aún no hay alias") {
		t.Fatalf("Spanish alias validation fragment = %q", aliasRec.Body.String())
	}

	mint := httptest.NewRequest(http.MethodPost, "/devices/add/token", strings.NewReader("autoSync=true&syncFrequency=30s"))
	mint.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mint.Host = "aliasdeck.test"
	mint.AddCookie(&http.Cookie{Name: languageCookieName, Value: "es"})
	mint = mint.WithContext(withSubject(mint.Context(), webSubject{TokenID: "browser-session-a", OperatorID: "operator-a"}))
	mintRec := httptest.NewRecorder()
	a.handleDevicesMintToken(mintRec, mint)
	body := mintRec.Body.String()
	if !strings.Contains(body, "Ejecutar en el nuevo equipo") || !strings.Contains(body, "Esperando a que el nuevo equipo") {
		t.Fatalf("Spanish enrollment fragment = %q", body)
	}
	if !strings.Contains(body, "aliasdeck agent install --interval") {
		t.Fatal("localized enrollment fragment changed the machine command")
	}
}

func TestSetupPageRendersSpanishAndPreservesCredentialPath(t *testing.T) {
	a, _, _ := newSetupWebapp(t)
	req := newSetupRequest(http.MethodGet, "/setup?lang=es", localSetupPeer, nil)
	rec := serveSetup(a, req)
	body := rec.Body.String()
	for _, want := range []string{`<html lang="es">`, "Configurar AliasDeck", "Confirmar contraseña", `name="return" value="/setup"`} {
		if !strings.Contains(body, want) {
			t.Errorf("setup body missing %q: %q", want, body)
		}
	}
}

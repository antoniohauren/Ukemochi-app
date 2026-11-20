package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testServer() (*Server, http.Handler) {
	s := &Server{Auth: &Auth{PIN: "4321", Key: []byte("0123456789abcdef0123456789abcdef")}}
	mux := http.NewServeMux()
	s.Routes(mux)
	mux.Handle("GET /admin/secreto", s.requireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})))
	return s, mux
}

func login(h http.Handler, pin string) *httptest.ResponseRecorder {
	form := url.Values{"pin": {pin}, "next": {"/admin/secreto"}}
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func getSecret(h http.Handler, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/admin/secreto", nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginWrongPIN(t *testing.T) {
	_, h := testServer()
	rec := login(h, "0000")
	if rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("wrong PIN: code %d, cookies %v", rec.Code, rec.Result().Cookies())
	}
}

func TestLoginAndAccess(t *testing.T) {
	_, h := testServer()
	rec := login(h, "4321")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/secreto" {
		t.Fatalf("login: code %d, location %q", rec.Code, rec.Header().Get("Location"))
	}
	c := rec.Result().Cookies()[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie flags: %+v", c)
	}
	if rec := getSecret(h, c.Value); rec.Code != http.StatusOK {
		t.Fatalf("with cookie: %d", rec.Code)
	}
}

func TestTamperedCookieRefused(t *testing.T) {
	s, h := testServer()
	good := s.Auth.newSession()
	exp, sig, _ := strings.Cut(good, "|")
	later := "9999999999"
	for name, v := range map[string]string{
		"none":          "",
		"garbage":       "abc",
		"later expiry":  later + "|" + sig,
		"flipped sig":   exp + "|" + strings.Repeat("0", len(sig)),
		"empty sig":     exp + "|",
		"other key sig": later + "|" + (&Auth{Key: []byte("x")}).sign(later),
	} {
		rec := getSecret(h, v)
		if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/login?next=") {
			t.Errorf("%s: code %d, location %q", name, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestExpiredSessionRefused(t *testing.T) {
	s, _ := testServer()
	v := s.Auth.newSession()
	s.Auth.now = func() time.Time { return time.Now().Add(sessionTTL + time.Minute) }
	if s.Auth.validSession(v) {
		t.Fatal("expired session accepted")
	}
}

func TestRateLimit(t *testing.T) {
	s, h := testServer()
	clock := time.Now()
	s.Auth.now = func() time.Time { return clock }
	for i := 0; i < maxFailures; i++ {
		login(h, "0000")
	}
	if rec := login(h, "4321"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("right PIN while limited: %d", rec.Code)
	}
	clock = clock.Add(failureWindow)
	if rec := login(h, "4321"); rec.Code != http.StatusSeeOther {
		t.Fatalf("after window: %d", rec.Code)
	}
}

func TestSuccessResetsFailures(t *testing.T) {
	_, h := testServer()
	for i := 0; i < maxFailures-1; i++ {
		login(h, "0000")
	}
	login(h, "4321")
	login(h, "0000")
	if rec := login(h, "4321"); rec.Code != http.StatusSeeOther {
		t.Fatalf("locked after a successful login: %d", rec.Code)
	}
}

func TestRequireAdminHTMX(t *testing.T) {
	_, h := testServer()
	req := httptest.NewRequest("GET", "/admin/secreto", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.HasPrefix(rec.Header().Get("HX-Redirect"), "/admin/login?next=") {
		t.Fatalf("htmx: code %d, HX-Redirect %q", rec.Code, rec.Header().Get("HX-Redirect"))
	}
}

func TestEmptyPINDisablesLogin(t *testing.T) {
	s, h := testServer()
	s.Auth.PIN = ""
	if rec := login(h, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("empty PIN login: %d", rec.Code)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/admin/cardapio":   "/admin/cardapio",
		"/admin":            "/admin",
		"":                  "/admin/relatorio",
		"https://evil.com":  "/admin/relatorio",
		"//evil.com/admin/": "/admin/relatorio",
		"/adminx":           "/admin/relatorio",
		"/admin/\\evil.com": "/admin/relatorio",
		"/pedidos/1":        "/admin/relatorio",
		"/admin/..//x":      "/admin/relatorio",
		"/admin/../pedidos": "/admin/relatorio",
		"/admin/./x":        "/admin/relatorio",
		"/admin/x?dia=1":    "/admin/x?dia=1",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDevUIRenders(t *testing.T) {
	_, h := testServer()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/dev/ui", nil))
	body := rec.Body.String()
	for _, want := range []string{"Enviar à cozinha", "Na cozinha", "R$ 36,00", `inputmode="decimal"`, "<dialog", `id="toast"`} {
		if !strings.Contains(body, want) {
			t.Errorf("/dev/ui missing %q", want)
		}
	}
}

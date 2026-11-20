package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "ukemochi_admin"
	sessionTTL    = 12 * time.Hour

	maxFailures   = 5
	failureWindow = 15 * time.Minute
)

// Auth checks the admin PIN and signs session cookies as "expiry|HMAC-SHA256(key, expiry)".
type Auth struct {
	PIN string // empty disables admin login
	Key []byte // HMAC key; at least 32 random bytes

	mu          sync.Mutex
	failures    int
	windowStart time.Time
	now         func() time.Time // for tests; nil means time.Now
}

func (a *Auth) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func (a *Auth) sign(expiry string) string {
	m := hmac.New(sha256.New, a.Key)
	m.Write([]byte(expiry))
	return hex.EncodeToString(m.Sum(nil))
}

// newSession returns a cookie value valid for sessionTTL.
func (a *Auth) newSession() string {
	exp := strconv.FormatInt(a.clock().Add(sessionTTL).Unix(), 10)
	return exp + "|" + a.sign(exp)
}

// validSession reports whether v carries a correct signature and has not expired.
func (a *Auth) validSession(v string) bool {
	exp, sig, ok := strings.Cut(v, "|")
	if !ok || len(a.Key) == 0 {
		return false
	}
	if !hmac.Equal([]byte(sig), []byte(a.sign(exp))) {
		return false
	}
	unix, err := strconv.ParseInt(exp, 10, 64)
	return err == nil && a.clock().Unix() < unix
}

// checkPIN compares the PIN and counts failures. The limit is shared by all clients:
// behind Caddy every request comes from 127.0.0.1, so a per-IP limit would not hold.
// shortcut: a flood of wrong PINs locks the real admin out for up to 15 min; track per X-Forwarded-For if that bites.
func (a *Auth) checkPIN(pin string) (ok, limited bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.clock()
	if now.Sub(a.windowStart) >= failureWindow {
		a.failures, a.windowStart = 0, now
	}
	if a.failures >= maxFailures {
		return false, true
	}
	if a.PIN != "" && subtle.ConstantTimeCompare([]byte(pin), []byte(a.PIN)) == 1 {
		a.failures = 0
		return true, false
	}
	a.failures++
	return false, false
}

// requireAdmin lets a request through only with a valid session cookie.
// Others are sent to the login page, which returns them to the page they asked for.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil && s.Auth.validSession(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		target := "/admin/login?next=" + url.QueryEscape(r.URL.RequestURI())
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", target)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	})
}

func (s *Server) registerAuth(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/login", func(w http.ResponseWriter, r *http.Request) {
		s.renderLogin(w, r, http.StatusOK, "")
	})
	mux.HandleFunc("POST /admin/login", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := r.ParseForm(); err != nil {
			s.renderLogin(w, r, http.StatusBadRequest, "Pedido inválido.")
			return
		}
		ok, limited := s.Auth.checkPIN(r.PostFormValue("pin"))
		switch {
		case limited:
			s.renderLogin(w, r, http.StatusTooManyRequests, "Muitas tentativas. Aguarde alguns minutos.")
			return
		case !ok:
			s.renderLogin(w, r, http.StatusUnauthorized, "PIN incorreto.")
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookie,
			Value:    s.Auth.newSession(),
			Path:     "/admin",
			MaxAge:   int(sessionTTL.Seconds()),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
		})
		http.Redirect(w, r, safeNext(r.PostFormValue("next")), http.StatusSeeOther)
	})
}

func (s *Server) renderLogin(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := LoginPage(safeNext(r.FormValue("next")), errMsg).Render(r.Context(), w); err != nil {
		log.Printf("render login: %v", err)
	}
}

// safeNext keeps redirects inside /admin so the login form can't send users off-site.
func safeNext(next string) string {
	if strings.ContainsAny(next, "\\\r\n") {
		return "/admin/relatorio"
	}
	// Clean resolves ".." so "/admin/../x" can't escape; compare without the query string.
	p, _, _ := strings.Cut(next, "?")
	if c := path.Clean(p); c == p && (c == "/admin" || strings.HasPrefix(c, "/admin/")) {
		return next
	}
	return "/admin/relatorio"
}

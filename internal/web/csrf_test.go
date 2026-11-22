package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCrossOrigin(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := CrossOrigin(ok)
	tests := []struct {
		name, site, origin string
		hx                 bool
		want               int
		body               string
	}{
		{name: "same origin", site: "same-origin", want: http.StatusNoContent},
		{name: "no fetch metadata, origin matches", origin: "https://pedidos.test", want: http.StatusNoContent},
		{name: "cross site form", site: "cross-site", want: http.StatusForbidden, body: "Recarregue"},
		{name: "cross site htmx", site: "cross-site", hx: true, want: http.StatusOK, body: "Recarregue"},
		{name: "old browser, origin differs", origin: "https://evil.test", want: http.StatusForbidden, body: "Recarregue"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "https://pedidos.test/pedidos", nil)
			if tt.site != "" {
				r.Header.Set("Sec-Fetch-Site", tt.site)
			}
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.hx {
				r.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
			if !strings.Contains(w.Body.String(), tt.body) {
				t.Fatalf("body %q does not contain %q", w.Body.String(), tt.body)
			}
			if tt.hx && w.Header().Get("HX-Reswap") != "none" {
				t.Fatal("htmx refusal must not swap")
			}
		})
	}
}

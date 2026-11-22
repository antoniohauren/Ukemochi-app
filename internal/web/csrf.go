package web

import (
	"log"
	"net/http"
)

// CrossOrigin wraps h in the stdlib cross-origin protection. A refused write is logged with the
// headers the check read, so a phone whose taps do nothing shows up in the server log, and the
// user sees a message instead of a silent no-op (htmx drops 403 responses).
func CrossOrigin(h http.Handler) http.Handler {
	p := http.NewCrossOriginProtection()
	p.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("cross-origin refused: %s %s host=%q origin=%q sec-fetch-site=%q ua=%q",
			r.Method, r.URL.Path, r.Host, r.Header.Get("Origin"), r.Header.Get("Sec-Fetch-Site"), r.UserAgent())
		const msg = "Pedido recusado pelo servidor. Recarregue a página e tente de novo."
		if isHX(r) {
			w.Header().Set("HX-Reswap", "none")
			render(w, r, http.StatusOK, Toast("error", msg))
			return
		}
		render(w, r, http.StatusForbidden, ErrorPage(msg))
	}))
	return p.Handler(h)
}

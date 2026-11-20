package web

import (
	"log"
	"net/http"
)

// registerDev serves the UI kit demo. It shows no data, so it is left open.
func (s *Server) registerDev(mux *http.ServeMux) {
	mux.HandleFunc("GET /dev/ui", func(w http.ResponseWriter, r *http.Request) {
		if err := DevUI().Render(r.Context(), w); err != nil {
			log.Printf("render dev ui: %v", err)
		}
	})
	mux.HandleFunc("GET /dev/ui/toast", func(w http.ResponseWriter, r *http.Request) {
		if err := Toast("ok", "Copiado!").Render(r.Context(), w); err != nil {
			log.Printf("render toast: %v", err)
		}
	})
}

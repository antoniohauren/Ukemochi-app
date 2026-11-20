// Package web holds HTTP handlers and templ views.
package web

import (
	"log"
	"net/http"

	"github.com/antoniohauren/ukemochi/internal/db"
)

// Server holds shared dependencies. Each feature file adds a register<Feature>(mux) method
// and a call to it in Routes, so parallel work stays in separate files.
type Server struct {
	DB   *db.DB
	Auth *Auth
}

// Routes registers every feature's routes on mux.
func (s *Server) Routes(mux *http.ServeMux) {
	s.registerHome(mux)
	s.registerDev(mux)
	s.registerAuth(mux)
}

func (s *Server) registerHome(mux *http.ServeMux) {
	mux.Handle("GET /{$}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := Home().Render(r.Context(), w); err != nil {
			log.Printf("render home: %v", err)
		}
	}))
}

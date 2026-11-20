// Package web holds HTTP handlers and templ views.
package web

import (
	"net/http"

	"github.com/antoniohauren/ukemochi/internal/db"
)

// Server holds shared dependencies. Each feature file adds a register<Feature>(mux) method
// and a call to it in Routes, so parallel work stays in separate files.
type Server struct {
	DB   *db.DB
	Auth *Auth
	// Print sends ESC/POS bytes to the printer. Nil means print from the phone's browser instead.
	Print func(data []byte) error
}

// Routes registers every feature's routes on mux.
func (s *Server) Routes(mux *http.ServeMux) {
	s.registerOrders(mux)
	s.registerMenu(mux)
	s.registerPrint(mux)
	s.registerPayment(mux)
	s.registerReport(mux)
	s.registerDev(mux)
	s.registerAuth(mux)
}

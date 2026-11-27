package web

import (
	"net/http"
	"time"
)

const closedURL = "/pedidos/fechados"

func (s *Server) registerClosed(mux *http.ServeMux) {
	mux.HandleFunc("GET "+closedURL, s.closedOrders)
}

// closedOrders lists one day's fechado and cancelado orders, so one can be found and reopened. No totals here:
// the money for the day is on the admin report.
func (s *Server) closedOrders(w http.ResponseWriter, r *http.Request) {
	day, err := reportDay(r.URL.Query().Get("dia"), time.Now())
	if err != nil {
		fail(w, r, err)
		return
	}
	orders, err := s.DB.ListClosedOrders(r.Context(), day, day.AddDate(0, 0, 1))
	if err != nil {
		fail(w, r, err)
		return
	}
	render(w, r, http.StatusOK, ClosedOrdersPage(day, orders))
}

func closedDayURL(day time.Time) string {
	return closedURL + "?dia=" + day.Format(dayLayout)
}

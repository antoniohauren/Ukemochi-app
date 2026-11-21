package web

import (
	"net/http"
	"strings"

	"github.com/antoniohauren/ukemochi/internal/db"
)

func (s *Server) registerCustomer(mux *http.ServeMux) {
	mux.HandleFunc("POST /pedidos/{id}/cliente", s.setCustomer)
}

// canEditCustomer reports whether o's name, phone and note may still change (db.SetCustomer).
func canEditCustomer(o db.Order) bool {
	return o.Status != db.StatusClosed && o.Status != db.StatusCancelled
}

// checkCustomer validates the client fields shared by the new-order form and the customer sheet.
// On error, field names the input at fault.
func checkCustomer(name, phone, notes string) (field string, err error) {
	switch {
	case name == "":
		return "nome", inputError("Informe o nome do cliente.")
	case len([]rune(name)) > 60:
		return "nome", inputError("Nome muito longo (máx. 60).")
	case phone != "" && !isDigits(phone, 4):
		return "telefone", inputError("Telefone: informe só os 4 últimos dígitos.")
	case len([]rune(notes)) > 200:
		return "obs", inputError("Observação muito longa (máx. 200).")
	}
	return "", nil
}

func (s *Server) setCustomer(w http.ResponseWriter, r *http.Request) {
	o, ok := s.loadOrder(w, r)
	if !ok {
		return
	}
	if err := parseForm(w, r); err != nil {
		fail(w, r, err)
		return
	}
	if !canEditCustomer(o) {
		fail(w, r, inputError("Pedido fechado ou cancelado: reabra para editar o cliente."))
		return
	}
	name := strings.TrimSpace(r.PostFormValue("nome"))
	phone := strings.TrimSpace(r.PostFormValue("telefone"))
	notes := strings.TrimSpace(r.PostFormValue("obs"))
	if _, err := checkCustomer(name, phone, notes); err != nil {
		fail(w, r, err)
		return
	}
	if err := s.DB.SetCustomer(r.Context(), o.ID, name, phone, notes); err != nil {
		fail(w, r, err)
		return
	}
	if !isHX(r) {
		redirect(w, r, orderURL(o.ID))
		return
	}
	o, err := s.DB.GetOrder(r.Context(), o.ID)
	if err != nil {
		fail(w, r, err)
		return
	}
	// Fired on the form before the swap, so the sheet can close and the actions sheet reopen.
	w.Header().Set("HX-Trigger", "cliente-salvo")
	render(w, r, http.StatusOK, CustomerUpdate(o))
}

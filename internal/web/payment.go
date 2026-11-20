package web

import (
	"net/http"
	"slices"
	"strings"

	"github.com/antoniohauren/ukemochi/internal/db"
	"github.com/antoniohauren/ukemochi/internal/money"
)

func (s *Server) registerPayment(mux *http.ServeMux) {
	mux.HandleFunc("POST /pedidos/{id}/pagamento", s.setPayment)
	mux.HandleFunc("GET /pedidos/{id}/troco", s.change)
}

// canPay reports whether o may still have its payment recorded or changed (docs/flows.md).
func canPay(o db.Order) bool { return o.Status != db.StatusClosed && o.Status != db.StatusCancelled }

func (s *Server) setPayment(w http.ResponseWriter, r *http.Request) {
	o, ok := s.loadOrder(w, r)
	if !ok {
		return
	}
	if err := parseForm(w, r); err != nil {
		fail(w, r, err)
		return
	}
	if !canPay(o) {
		fail(w, r, inputError("Pedido fechado ou cancelado: reabra para alterar o pagamento."))
		return
	}
	method := r.PostFormValue("metodo")
	if !slices.Contains(db.PaymentMethods, method) {
		fail(w, r, inputError("Escolha a forma de pagamento."))
		return
	}
	var given int64
	if v := strings.TrimSpace(r.PostFormValue("valor")); method == "dinheiro" && v != "" {
		var err error
		if given, err = money.Parse(v); err != nil {
			fail(w, r, inputError("Valor recebido inválido."))
			return
		}
		if _, ok := money.Change(o.TotalCents, given); !ok {
			fail(w, r, inputError("Valor recebido menor que o total."))
			return
		}
	}
	if err := s.DB.SetPayment(r.Context(), o.ID, method, given); err != nil {
		fail(w, r, err)
		return
	}
	redirect(w, r, orderURL(o.ID))
}

// change answers the live change line under the cash field.
func (s *Server) change(w http.ResponseWriter, r *http.Request) {
	o, ok := s.loadOrder(w, r)
	if !ok {
		return
	}
	render(w, r, http.StatusOK, ChangeLine(changeText(o.TotalCents, r.URL.Query().Get("valor"))))
}

// changeText describes the change for a typed cash amount; empty input gives an empty line.
func changeText(total int64, typed string) (text string, ok bool) {
	if strings.TrimSpace(typed) == "" {
		return "", true
	}
	given, err := money.Parse(typed)
	if err != nil {
		return "Valor inválido", false
	}
	c, enough := money.Change(total, given)
	if !enough {
		return "Faltam " + money.Format(total-given), false
	}
	return "Troco: " + money.Format(c), true
}

package web

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/antoniohauren/ukemochi/internal/db"
	"github.com/antoniohauren/ukemochi/internal/money"
)

func (s *Server) registerPayment(mux *http.ServeMux) {
	mux.HandleFunc("POST /pedidos/{id}/pagamento", s.addPayment)
	mux.HandleFunc("DELETE /pedidos/{id}/pagamentos/{pagamento}", s.deletePayment)
	mux.HandleFunc("POST /pedidos/{id}/pagamentos/{pagamento}", s.deletePayment) // form fallback without htmx
	mux.HandleFunc("GET /pedidos/{id}/troco", s.change)
}

// canPay reports whether o may still have its payment recorded or changed (docs/flows.md).
func canPay(o db.Order) bool { return o.Status != db.StatusClosed && o.Status != db.StatusCancelled }

// canAddPayment reports whether the payment sheet offers another part: something is left to pay,
// or the order costs nothing and has no part yet.
func canAddPayment(o db.Order) bool {
	return canPay(o) && (o.RemainingCents() > 0 || o.TotalCents == 0 && o.PaymentCount == 0)
}

// addPayment records one part of the bill. An empty valor pays everything that is left.
func (s *Server) addPayment(w http.ResponseWriter, r *http.Request) {
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
	rest := o.RemainingCents()
	amount := rest
	if v := strings.TrimSpace(r.PostFormValue("valor")); v != "" {
		var err error
		if amount, err = money.Parse(v); err != nil {
			fail(w, r, inputError("Valor a pagar inválido."))
			return
		}
	}
	switch {
	case !canAddPayment(o):
		fail(w, r, inputError("O pedido já está pago."))
		return
	case amount > rest:
		fail(w, r, inputError("Valor maior que o que falta pagar ("+money.Format(rest)+")."))
		return
	case amount <= 0 && rest > 0:
		fail(w, r, inputError("Informe o valor a pagar."))
		return
	}
	var given int64
	if v := strings.TrimSpace(r.PostFormValue("recebido")); method == "dinheiro" && v != "" {
		var err error
		if given, err = money.Parse(v); err != nil {
			fail(w, r, inputError("Valor recebido inválido."))
			return
		}
		if given < amount {
			fail(w, r, inputError("Valor recebido menor que o valor a pagar."))
			return
		}
	}
	if err := s.DB.AddPayment(r.Context(), o.ID, method, amount, given, parsePeople(r.PostFormValue("pessoas"))); err != nil {
		fail(w, r, err)
		return
	}
	s.paymentDone(w, r, o.ID, "Pagamento registrado")
}

// maxPeople caps the split picker.
const maxPeople = 20

// parsePeople reads the split picker; anything invalid means one person. It only sets the next default amount,
// so it is never an error.
func parsePeople(v string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n < 1 {
		return 1
	}
	return min(n, maxPeople)
}

func (s *Server) deletePayment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	pid, okP := pathID(r, "pagamento")
	if !ok || !okP {
		fail(w, r, db.ErrNotFound)
		return
	}
	if err := s.DB.DeletePayment(r.Context(), id, pid); err != nil {
		fail(w, r, err)
		return
	}
	s.paymentDone(w, r, id, "Pagamento removido")
}

// paymentDone answers a payment change: the sheet stays open for the next part under htmx, with the split
// picker still on the order's stored split, otherwise the browser goes back to the order.
func (s *Server) paymentDone(w http.ResponseWriter, r *http.Request, id int64, toast string) {
	if !isHX(r) {
		redirect(w, r, orderURL(id))
		return
	}
	o, err := s.DB.GetOrder(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	render(w, r, http.StatusOK, PaymentUpdate(o, toast))
}

// change answers the live change line under the cash field: recebido against valor (or all that is left).
func (s *Server) change(w http.ResponseWriter, r *http.Request) {
	o, ok := s.loadOrder(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	amount := o.RemainingCents()
	if v := strings.TrimSpace(q.Get("valor")); v != "" {
		var err error
		if amount, err = money.Parse(v); err != nil {
			render(w, r, http.StatusOK, ChangeLine("Valor a pagar inválido", "—", false))
			return
		}
	}
	render(w, r, http.StatusOK, ChangeLine(changeText(amount, q.Get("recebido"))))
}

// changeText describes the change for a typed cash amount; empty input gives an empty line.
// The label sits on the left and the amount on the right, in color (ok green, short red).
func changeText(amount int64, typed string) (label, shown string, ok bool) {
	if strings.TrimSpace(typed) == "" {
		return "", "", true
	}
	given, err := money.Parse(typed)
	if err != nil {
		return "Valor inválido", "—", false
	}
	c, enough := money.Change(amount, given)
	if !enough {
		return "Faltam", money.Format(amount - given), false
	}
	return "Troco", money.Format(c), true
}

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
	mux.HandleFunc("POST /pedidos/{id}/pagamento-previsto", s.setExpectedPayment)
	mux.HandleFunc("POST /pedidos/{id}/itens/{item}/partes", s.setItemSplit)
}

// setItemSplit splits a shared line into partes for Por consumo (a 1L soda for three), then answers like a
// payment change, so the sheet stays on Por consumo.
func (s *Server) setItemSplit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	item, okI := pathID(r, "item")
	if !ok || !okI {
		fail(w, r, db.ErrNotFound)
		return
	}
	if err := parseForm(w, r); err != nil {
		fail(w, r, err)
		return
	}
	parts, err := strconv.ParseInt(strings.TrimSpace(r.PostFormValue("partes")), 10, 64)
	if err != nil || parts < 1 || parts > db.MaxSplitParts {
		fail(w, r, inputError("Divida o item entre 1 e "+strconv.Itoa(db.MaxSplitParts)+" pessoas."))
		return
	}
	if err := s.DB.SetItemSplit(r.Context(), id, item, parts); err != nil {
		fail(w, r, err)
		return
	}
	toast := "Item dividido em " + strconv.FormatInt(parts, 10)
	if parts == 1 {
		toast = "Item sem divisão"
	}
	s.paymentDone(w, r, id, toast)
}

// setExpectedPayment records how the client will pay what is left, for the ticket. An empty previsto clears it.
// troco_para is the note a dinheiro client pays with; it must cover what is left.
func (s *Server) setExpectedPayment(w http.ResponseWriter, r *http.Request) {
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
	method := r.PostFormValue("previsto")
	if method != "" && !slices.Contains(db.PaymentMethods, method) {
		fail(w, r, inputError("Forma de pagamento prevista inválida."))
		return
	}
	if method != "" && o.Consume != db.ConsumeDelivery {
		fail(w, r, inputError("Forma prevista só vale para entrega: no local, registre o pagamento."))
		return
	}
	var changeFor int64
	if v := strings.TrimSpace(r.PostFormValue("troco_para")); method == "dinheiro" && v != "" {
		var err error
		if changeFor, err = money.Parse(v); err != nil || changeFor <= 0 {
			fail(w, r, inputError("Valor de troco para inválido."))
			return
		}
		if rest := o.RemainingCents(); changeFor < rest {
			fail(w, r, inputError("Troco para menor que o valor a pagar ("+money.Format(rest)+")."))
			return
		}
	}
	if err := s.DB.SetExpectedPayment(r.Context(), o.ID, method, changeFor); err != nil {
		fail(w, r, err)
		return
	}
	if isHX(r) && r.Header.Get("HX-Target") == "pagamento-entrega" {
		// From the Pagamento na entrega sheet: swap it, refresh the rest, and go back to the actions sheet.
		o, err := s.DB.GetOrder(r.Context(), o.ID)
		if err != nil {
			fail(w, r, err)
			return
		}
		w.Header().Set("HX-Trigger", "previsto-salvo")
		render(w, r, http.StatusOK, DeliveryPaymentUpdate(o, "Pagamento na entrega salvo"))
		return
	}
	s.paymentDone(w, r, o.ID, "Forma prevista salva")
}

// payOnDelivery reports whether o is a delivery that still has something for the courier to collect.
func payOnDelivery(o db.Order) bool {
	return canPay(o) && o.Consume == db.ConsumeDelivery && o.RemainingCents() > 0
}

// expectedText describes the expected payment of what is left: "Dinheiro · troco para R$ 100,00 (levar R$ 64,00)".
// It is empty when nothing is left to pay, no method is set or the order is eaten in (paid at the counter).
func expectedText(o db.Order) string {
	rest := o.RemainingCents()
	if rest <= 0 || o.ExpectedMethod == "" || o.Consume != db.ConsumeDelivery {
		return ""
	}
	t := paymentLabels[o.ExpectedMethod]
	if o.ChangeForCents > 0 {
		t += " · troco para " + money.Format(o.ChangeForCents)
		if o.ChangeForCents >= rest {
			t += " (levar " + money.Format(o.ChangeForCents-rest) + ")"
		}
	}
	return t
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
	// modo is the sheet's Tudo / Dividir / Por consumo button. Without it (no form buttons on a free order),
	// picked units or a typed valor decide.
	mode := r.PostFormValue("modo")
	if mode != "" && !slices.Contains(allPaymentModes, mode) {
		fail(w, r, inputError("Escolha como pagar."))
		return
	}
	var units map[int64]int64
	if mode == "" || mode == modeConsumption {
		var err error
		if units, err = parseUnits(r, o); err != nil {
			fail(w, r, err)
			return
		}
	}
	if mode == modeConsumption && len(units) == 0 {
		fail(w, r, inputError("Marque o que esta pessoa consumiu."))
		return
	}
	people := parsePeople(r.PostFormValue("pessoas"))
	if mode == modeAll {
		people = 1
	}
	rest := o.RemainingCents()
	amount := rest
	if len(units) > 0 {
		// Por consumo: the amount is what was picked, never the typed valor.
		amount = unitsTotal(o, units)
	} else if v := strings.TrimSpace(r.PostFormValue("valor")); mode != modeAll && v != "" {
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
	var err error
	if len(units) > 0 {
		_, err = s.DB.AddConsumptionPayment(r.Context(), o.ID, method, given, units)
	} else {
		err = s.DB.AddPayment(r.Context(), o.ID, method, amount, given, people)
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	s.paymentDone(w, r, o.ID, "Pagamento registrado")
}

// Payment modes: the sheet's Como pagar buttons, each showing its own screen.
const (
	modeAll         = "tudo"    // all that is left
	modeSplit       = "dividir" // an equal share, or a typed amount
	modeConsumption = "consumo" // the units one person had
)

var allPaymentModes = []string{modeAll, modeSplit, modeConsumption}

var paymentModeLabels = map[string]string{modeAll: "Tudo", modeSplit: "Dividir", modeConsumption: "Por Consumo"}

// paymentModes lists the modes the sheet offers; Por consumo only while some unit is still unpaid.
func paymentModes(o db.Order) []string {
	if canPayByConsumption(o) {
		return allPaymentModes
	}
	return allPaymentModes[:2]
}

// paymentMode is the mode the sheet opens on: how the bill was being split so far, Tudo otherwise.
func paymentMode(o db.Order) string {
	switch {
	case (paidByConsumption(o) || hasSplitItem(o)) && canPayByConsumption(o):
		return modeConsumption
	case o.SplitPeople > 1:
		return modeSplit
	}
	return modeAll
}

// unitsField names the Por consumo stepper of one order line.
func unitsField(itemID int64) string { return "unidades-" + strconv.FormatInt(itemID, 10) }

// parseUnits reads the Por consumo steppers: how many units of each line this part pays. Empty and 0 are skipped;
// more than the line still has unpaid is an error.
func parseUnits(r *http.Request, o db.Order) (map[int64]int64, error) {
	units := map[int64]int64{}
	for _, it := range o.Items {
		v := strings.TrimSpace(r.PostFormValue(unitsField(it.ID)))
		if v == "" || v == "0" {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 || n > it.UnpaidQty() {
			return nil, inputError("Quantidade inválida para " + it.NameSnapshot + ".")
		}
		if n > 0 {
			units[it.ID] = n
		}
	}
	return units, nil
}

// unitsTotal is what the picked units cost, in centavos.
func unitsTotal(o db.Order, units map[int64]int64) int64 {
	var total int64
	for _, it := range o.Items {
		total += it.PartsCost(it.PaidQty, units[it.ID])
	}
	return total
}

// hasSplitItem reports whether a line is split into parts, so the sheet opens on Por consumo.
func hasSplitItem(o db.Order) bool {
	return slices.ContainsFunc(o.Items, func(it db.OrderItem) bool { return it.SplitParts > 1 })
}

// partsText counts parts of a line: "2" for whole units, "1/3" for a third of a split unit.
func partsText(n, split int64) string {
	if split <= 1 {
		return strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10) + "/" + strconv.FormatInt(split, 10)
}

// canPayByConsumption reports whether the Por consumo picker has something to offer.
func canPayByConsumption(o db.Order) bool {
	return slices.ContainsFunc(o.Items, func(it db.OrderItem) bool { return it.UnpaidQty() > 0 })
}

// paidByConsumption reports whether a part was already paid by consumption, so the picker opens by itself.
func paidByConsumption(o db.Order) bool {
	return slices.ContainsFunc(o.Payments, func(p db.Payment) bool { return len(p.Items) > 0 })
}

// paymentItemsText lists what a part paid by consumption covers: "2x Hot roll · 1/3 Coca-Cola 1L".
func paymentItemsText(p db.Payment) string {
	parts := make([]string, len(p.Items))
	for i, it := range p.Items {
		if it.SplitParts > 1 {
			parts[i] = partsText(it.Qty, it.SplitParts) + " " + it.Name
		} else {
			parts[i] = strconv.FormatInt(it.Qty, 10) + "x " + it.Name
		}
	}
	return strings.Join(parts, " · ")
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

// splitVals is the hx-vals of a Dividir item entre button.
func splitVals(parts int64) string { return `{"partes":"` + strconv.FormatInt(parts, 10) + `"}` }

// consumptionText describes a line in the Por consumo picker: its price, how it is divided and what is left,
// e.g. "R$ 15,00 · 3 no pedido" or "R$ 12,00 ÷ 3 = R$ 4,00 cada · falta 2 de 3".
func consumptionText(it db.OrderItem) string {
	t := money.Format(it.UnitPriceCents)
	if it.SplitParts > 1 {
		t += " ÷ " + strconv.FormatInt(it.SplitParts, 10) + " = " + money.Format(it.PartsCost(0, 1)) + " cada"
	}
	switch left := it.UnpaidQty(); {
	case left == 0:
		return t + " · pago"
	case it.PaidQty > 0:
		return t + " · falta " + strconv.FormatInt(left, 10) + " de " + strconv.FormatInt(it.Parts(), 10)
	case it.SplitParts > 1:
		return t + " · " + strconv.FormatInt(it.Parts(), 10) + " partes"
	}
	return t + " · " + strconv.FormatInt(it.Qty, 10) + " no pedido"
}

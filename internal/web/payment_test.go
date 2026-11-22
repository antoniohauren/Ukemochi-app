package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/antoniohauren/ukemochi/internal/db"
)

// orderWith creates an order holding qty Hot rolls (R$ 15,00 each).
func orderWith(t *testing.T, s *Server, prod, qty int64) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := s.DB.CreateOrder(ctx, db.NewOrder{Channel: "balcao", Consume: db.ConsumeLocal, ClientName: "Ana"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.AddItem(ctx, id, prod, qty, ""); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestChangeText(t *testing.T) {
	for _, tc := range []struct {
		typed, label, amount string
		ok                   bool
	}{
		{"", "", "", true},
		{"50", "Troco", "R$ 20,00", true},
		{"30,00", "Troco", "R$ 0,00", true},
		{"20", "Faltam", "R$ 10,00", false},
		{"abc", "Valor inválido", "—", false},
	} {
		if label, amount, ok := changeText(3000, tc.typed); label != tc.label || amount != tc.amount || ok != tc.ok {
			t.Errorf("changeText(3000, %q) = %q, %q, %v; want %q, %q, %v", tc.typed, label, amount, ok, tc.label, tc.amount, tc.ok)
		}
	}
}

func TestChangeEndpoint(t *testing.T) {
	s, h, prod := dbServer(t)
	id := orderWith(t, s, prod, 2) // R$ 30,00
	for _, tc := range []struct{ query, want string }{
		{"recebido=50", "R$ 20,00"},          // against all that is left
		{"valor=10&recebido=50", "R$ 40,00"}, // against this part
		{"valor=x&recebido=50", "Valor a pagar inválido"},
	} {
		rec := do(h, "GET", orderURL(id)+"/troco?"+tc.query, nil, true)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: code %d, body %s", tc.query, rec.Code, rec.Body)
		}
	}
	if rec := do(h, "GET", "/pedidos/999/troco?recebido=1", nil, true); !strings.Contains(rec.Body.String(), "Não encontrado") {
		t.Errorf("missing order: %s", rec.Body)
	}
}

func TestPayment(t *testing.T) {
	s, h, prod := dbServer(t)
	ctx := context.Background()
	id := orderWith(t, s, prod, 2) // R$ 30,00
	for _, st := range []string{db.StatusKitchen, db.StatusReady} {
		if err := s.DB.SetStatus(ctx, id, st); err != nil {
			t.Fatal(err)
		}
	}

	page := do(h, "GET", orderURL(id), nil, false).Body.String()
	if !strings.Contains(page, "Registre o pagamento para fechar") || !regexp.MustCompile(`<button[^>]* disabled[ >][^>]*>\s*Fechar`).MatchString(page) {
		t.Error("editor should disable the close button and explain why")
	}
	if !strings.Contains(page, `id="pagamento-sheet"`) || !strings.Contains(page, "pagamento-sheet&#39;).showModal()") {
		t.Error("payment form should live in its own sheet, opened from the order panel")
	}
	if !strings.Contains(page, "document.getElementById(&#39;pagamento-sheet&#39;).close(); document.getElementById(&#39;acoes-sheet&#39;).showModal()") {
		t.Error("payment sheet should have a Voltar back to the actions sheet")
	}
	for _, want := range []string{`name="pessoas" value="1"`, `data-total="3000"`, `data-split-step="+"`, `name="valor" value="30,00"`} {
		if !strings.Contains(page, want) {
			t.Errorf("payment sheet lacks the people picker: %s", want)
		}
	}

	// An error from the payment sheet is repeated inside it, since the sheet covers the toast.
	req := httptest.NewRequest("POST", orderURL(id)+"/pagamento", strings.NewReader("metodo=cheque"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "pagamento")
	sheetRec := httptest.NewRecorder()
	h.ServeHTTP(sheetRec, req)
	if b := sheetRec.Body.String(); !strings.Contains(b, `id="pagamento-erro"`) || strings.Count(b, "Escolha a forma") != 2 {
		t.Errorf("sheet error: %s", b)
	}

	for _, tc := range []struct {
		name string
		form url.Values
	}{
		{"no method", url.Values{}},
		{"bad method", url.Values{"metodo": {"cheque"}}},
		{"bad amount", url.Values{"metodo": {"pix"}, "valor": {"12,345"}}},
		{"zero amount", url.Values{"metodo": {"pix"}, "valor": {"0"}}},
		{"over the total", url.Values{"metodo": {"pix"}, "valor": {"30,01"}}},
		{"bad cash", url.Values{"metodo": {"dinheiro"}, "recebido": {"12,345"}}},
		{"cash short", url.Values{"metodo": {"dinheiro"}, "recebido": {"29,99"}}},
		{"cash short of the part", url.Values{"metodo": {"dinheiro"}, "valor": {"10"}, "recebido": {"9"}}},
	} {
		if rec := do(h, "POST", orderURL(id)+"/pagamento", tc.form, false); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: code %d", tc.name, rec.Code)
		}
	}
	if o, _ := s.DB.GetOrder(ctx, id); o.PaymentCount != 0 {
		t.Fatalf("payment recorded after invalid input: %+v", o.Payments)
	}

	// Three people split it: the picker stays on 3 after the first share, so the next amount is a share too.
	other := orderWith(t, s, prod, 2)
	rec := do(h, "POST", orderURL(other)+"/pagamento", url.Values{"metodo": {"pix"}, "valor": {"10"}, "pessoas": {"3"}}, true)
	if b := rec.Body.String(); !strings.Contains(b, `name="pessoas" value="3"`) || !strings.Contains(b, `name="valor" value="10,00"`) ||
		!strings.Contains(b, "R$ 20,00") {
		t.Fatalf("split share: %s", b)
	}
	for v, want := range map[string]int64{"": 1, "x": 1, "0": 1, "4": 4, "99": maxPeople} {
		if got := parsePeople(v); got != want {
			t.Errorf("parsePeople(%q) = %d, want %d", v, got, want)
		}
	}

	// Split in two: R$ 12,00 by PIX, then the rest in cash. The sheet stays open between parts.
	rec = do(h, "POST", orderURL(id)+"/pagamento", url.Values{"metodo": {"pix"}, "valor": {"12"}}, true)
	b := rec.Body.String()
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "" || !strings.Contains(b, `id="pagamento"`) ||
		!strings.Contains(b, "Falta") || !strings.Contains(b, "R$ 18,00") || !strings.Contains(b, `id="acoes" hx-swap-oob="true"`) ||
		!strings.Contains(b, "Pagamento registrado") {
		t.Fatalf("first part: code %d, headers %v, body %s", rec.Code, rec.Header(), b)
	}
	if !strings.Contains(b, `name="valor" value="18,00"`) {
		t.Error("amount field should default to what is left")
	}
	page = do(h, "GET", orderURL(id), nil, false).Body.String()
	if !strings.Contains(page, "Os pagamentos não batem com o total") {
		t.Error("close button should explain a partial payment")
	}
	// An empty amount pays everything left.
	if rec := do(h, "POST", orderURL(id)+"/pagamento", url.Values{"metodo": {"dinheiro"}, "recebido": {"20"}}, false); rec.Code != http.StatusSeeOther {
		t.Fatalf("second part: code %d, body %s", rec.Code, rec.Body)
	}
	o, _ := s.DB.GetOrder(ctx, id)
	if !o.Paid() || len(o.Payments) != 2 || o.Payments[1].AmountCents != 1800 || o.Payments[1].CashGivenCents != 2000 {
		t.Fatalf("payments = %+v", o.Payments)
	}
	page = do(h, "GET", orderURL(id), nil, false).Body.String()
	if !strings.Contains(page, "Troco R$ 2,00") || !strings.Contains(page, "Pago em 2 partes") || strings.Contains(page, "Registre o pagamento para fechar") {
		t.Error("editor should show the parts, the change and an enabled close button")
	}
	if strings.Contains(page, `name="metodo"`) {
		t.Error("a paid order should not offer another part")
	}
	if rec := do(h, "POST", orderURL(id)+"/pagamento", url.Values{"metodo": {"pix"}}, false); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("paying a paid order: code %d", rec.Code)
	}

	// Removing a part, with htmx and without.
	rec = do(h, "DELETE", paymentURL(id, o.Payments[1].ID), nil, true)
	if b := rec.Body.String(); !strings.Contains(b, "Pagamento removido") || !strings.Contains(b, "R$ 18,00") {
		t.Fatalf("remove part: %s", b)
	}
	if rec := do(h, "POST", paymentURL(id, o.Payments[0].ID), nil, false); rec.Code != http.StatusSeeOther {
		t.Fatalf("remove part without htmx: code %d", rec.Code)
	}
	if rec := do(h, "POST", paymentURL(id, 999), nil, false); rec.Code != http.StatusNotFound {
		t.Errorf("remove missing part: code %d", rec.Code)
	}
	if o, _ := s.DB.GetOrder(ctx, id); o.PaymentCount != 0 {
		t.Fatalf("payments left: %+v", o.Payments)
	}

	if err := s.DB.SetStatus(ctx, id, db.StatusCancelled); err != nil {
		t.Fatal(err)
	}
	if rec := do(h, "POST", orderURL(id)+"/pagamento", url.Values{"metodo": {"pix"}}, false); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("paying a cancelled order: code %d", rec.Code)
	}
}

func TestPaymentOverpaidAfterReopen(t *testing.T) {
	s, h, prod := dbServer(t)
	ctx := context.Background()
	id := orderWith(t, s, prod, 2) // R$ 30,00
	if err := s.DB.AddPayment(ctx, id, "pix", 3000, 0, 1); err != nil {
		t.Fatal(err)
	}
	o, _ := s.DB.GetOrder(ctx, id)
	if err := s.DB.SetItemQty(ctx, id, o.Items[0].ID, 1); err != nil {
		t.Fatal(err)
	}
	page := do(h, "GET", orderURL(id), nil, false).Body.String()
	if !strings.Contains(page, "Pago R$ 15,00 a mais") {
		t.Error("editor should flag a payment above the total")
	}
	if err := s.DB.SetStatus(ctx, id, db.StatusKitchen); err != nil {
		t.Fatal(err)
	}
	s.DB.SetStatus(ctx, id, db.StatusReady)
	if rec := do(h, "POST", orderURL(id)+"/status", url.Values{"para": {db.StatusClosed}}, false); rec.Code != http.StatusConflict {
		t.Errorf("closing an overpaid order: code %d", rec.Code)
	}
}

// The split is stored with each part, so the sheet reopens on it after a reload or on another phone.
func TestPaymentSplitKept(t *testing.T) {
	s, h, prod := dbServer(t)
	id := orderWith(t, s, prod, 8) // R$ 120,00
	if rec := do(h, "POST", orderURL(id)+"/pagamento", url.Values{"metodo": {"pix"}, "valor": {"40"}, "pessoas": {"3"}}, true); rec.Code != http.StatusOK {
		t.Fatalf("pay: %d", rec.Code)
	}
	b := do(h, "GET", orderURL(id), nil, false).Body.String()
	for _, want := range []string{`name="pessoas" value="3"`, `name="valor" value="40,00"`, "Parte 2 de 3"} {
		if !strings.Contains(b, want) {
			t.Errorf("fresh GET lacks %s", want)
		}
	}
}

// The last part of a split fills all that is left, even when the total grew mid-split.
func TestPaymentLastPartTakesRest(t *testing.T) {
	s, h, prod := dbServer(t)
	id := orderWith(t, s, prod, 8) // R$ 120,00
	for range 2 {
		if rec := do(h, "POST", orderURL(id)+"/pagamento", url.Values{"metodo": {"pix"}, "valor": {"40"}, "pessoas": {"3"}}, true); rec.Code != http.StatusOK {
			t.Fatalf("pay: %d", rec.Code)
		}
	}
	do(h, "POST", orderURL(id)+"/itens", url.Values{"produto": {itoa(prod)}}, false) // R$ 135,00, R$ 55,00 left
	b := do(h, "GET", orderURL(id), nil, false).Body.String()
	for _, want := range []string{"Parte 3 de 3", `name="valor" value="55,00"`, `data-paid="2"`} {
		if !strings.Contains(b, want) {
			t.Errorf("fresh GET lacks %s", want)
		}
	}
	// Removing a part asks first.
	if !strings.Contains(b, `hx-confirm="Remover pagamento PIX de R$ 40,00?"`) {
		t.Errorf("remove payment does not ask")
	}
}

// The expected payment goes on the ticket next to A PAGAR and never counts as paid.
func TestExpectedPayment(t *testing.T) {
	s, h, prod := dbServer(t)
	id := orderWith(t, s, prod, 2) // R$ 30,00
	u := orderURL(id) + "/pagamento-previsto"
	for _, tc := range []struct {
		name string
		form url.Values
		want string
	}{
		{"bad method", url.Values{"previsto": {"cheque"}}, "Forma de pagamento prevista inválida"},
		{"bad note", url.Values{"previsto": {"dinheiro"}, "troco_para": {"abc"}}, "Valor de troco para inválido"},
		{"note short", url.Values{"previsto": {"dinheiro"}, "troco_para": {"20"}}, "Troco para menor que o valor a pagar (R$ 30,00)"},
	} {
		if b := do(h, "POST", u, tc.form, true).Body.String(); !strings.Contains(b, tc.want) {
			t.Errorf("%s: %s", tc.name, b)
		}
	}

	if rec := do(h, "POST", u, url.Values{"previsto": {"dinheiro"}, "troco_para": {"100"}}, true); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Forma prevista salva") {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	o, _ := s.DB.GetOrder(context.Background(), id)
	if o.ExpectedMethod != "dinheiro" || o.ChangeForCents != 10000 || o.PaymentCount != 0 {
		t.Fatalf("stored: %+v", o)
	}
	if b := do(h, "GET", orderURL(id), nil, false).Body.String(); !strings.Contains(b, "Previsto: Dinheiro · troco para R$ 100,00 (levar R$ 70,00)") {
		t.Error("order panel lacks the expected payment")
	}
	txt := ticketText(o)
	for _, want := range []string{"A PAGAR\nPagar com", "Pagar com               Dinheiro", "Troco para             R$ 100,00", "Levar troco             R$ 70,00"} {
		if !strings.Contains(txt, want) {
			t.Errorf("ticket lacks %q:\n%s", want, txt)
		}
	}

	// Another method drops the note; an empty one clears it all.
	do(h, "POST", u, url.Values{"previsto": {"credito"}, "troco_para": {"100"}}, true)
	if o, _ := s.DB.GetOrder(context.Background(), id); o.ExpectedMethod != "credito" || o.ChangeForCents != 0 {
		t.Fatalf("credito: %+v", o)
	}
	do(h, "POST", u, url.Values{"previsto": {""}}, true)
	if o, _ := s.DB.GetOrder(context.Background(), id); o.ExpectedMethod != "" || strings.Contains(ticketText(o), "Pagar com") {
		t.Fatalf("cleared: %+v", o)
	}

	// Once paid, the ticket has nothing left to collect.
	do(h, "POST", u, url.Values{"previsto": {"pix"}}, true)
	do(h, "POST", orderURL(id)+"/pagamento", url.Values{"metodo": {"pix"}}, true)
	if o, _ := s.DB.GetOrder(context.Background(), id); strings.Contains(ticketText(o), "Pagar com") {
		t.Errorf("paid ticket still asks to collect:\n%s", ticketText(o))
	}
	if rec := do(h, "POST", orderURL(99)+"/pagamento-previsto", url.Values{"previsto": {"pix"}}, false); rec.Code != http.StatusNotFound {
		t.Errorf("missing order: %d", rec.Code)
	}
}

// A delivery with something left to pay offers Paga na entrega, its own sheet for the expected payment;
// a save there swaps that sheet and goes back to the actions sheet. A local order keeps Forma prevista.
func TestPayOnDeliverySheet(t *testing.T) {
	s, h, prod := dbServer(t)
	do(h, "POST", "/pedidos", newOrderForm("Maria"), false)
	if err := s.DB.AddItem(context.Background(), 1, prod, 1, ""); err != nil {
		t.Fatal(err)
	}
	b := do(h, "GET", orderURL(1), nil, false).Body.String()
	for _, want := range []string{"Já pagou", "Paga na entrega", `id="entrega-pagamento-sheet"`, `id="pagamento-entrega"`, "entrega-pagamento-sheet&#39;).showModal()"} {
		if !strings.Contains(b, want) {
			t.Errorf("delivery order lacks %q", want)
		}
	}
	if strings.Contains(b, "Forma prevista (ainda não pago)") {
		t.Error("a delivery should set the expected payment in its own sheet, not the payment sheet")
	}

	req := httptest.NewRequest("POST", orderURL(1)+"/pagamento-previsto", strings.NewReader(url.Values{"previsto": {"dinheiro"}, "troco_para": {"abc"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "pagamento-entrega")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `id="pagamento-entrega-erro"`) {
		t.Errorf("error should show inside the sheet: %s", rec.Body)
	}

	req = httptest.NewRequest("POST", orderURL(1)+"/pagamento-previsto", strings.NewReader(url.Values{"previsto": {"pix"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "pagamento-entrega")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if b := rec.Body.String(); rec.Header().Get("HX-Trigger") != "previsto-salvo" || !strings.HasPrefix(b, `<div id="pagamento-entrega"`) || !strings.Contains(b, "Pagamento na entrega salvo") {
		t.Errorf("save: %q %s", rec.Header().Get("HX-Trigger"), b)
	}
	if b := do(h, "GET", orderURL(1), nil, false).Body.String(); !strings.Contains(b, "Paga na entrega: PIX") {
		t.Error("order panel should say how the client pays on delivery")
	}

	id := orderWith(t, s, prod, 1)
	b = do(h, "GET", orderURL(id), nil, false).Body.String()
	if strings.Contains(b, "Já pagou") || !strings.Contains(b, "Forma prevista (ainda não pago)") {
		t.Error("a local order should keep Forma prevista in the payment sheet")
	}
}

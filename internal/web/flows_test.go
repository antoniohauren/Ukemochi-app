package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestFlows walks the three flows of docs/flows.md through HTTP only, then checks the daily report.
func TestFlows(t *testing.T) {
	s, h, prod := dbServer(t)

	step := func(method, target string, form url.Values, want int) string {
		t.Helper()
		rec := do(h, method, target, form, false)
		if rec.Code != want {
			t.Fatalf("%s %s: code %d, want %d: %s", method, target, rec.Code, want, rec.Body)
		}
		return rec.Body.String()
	}
	create := func(form url.Values) string {
		t.Helper()
		rec := do(h, "POST", "/pedidos", form, false)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("create: code %d: %s", rec.Code, rec.Body)
		}
		return rec.Header().Get("Location")
	}
	status := func(o, to string) {
		t.Helper()
		step("POST", o+"/status", url.Values{"para": {to}}, http.StatusSeeOther)
	}
	pay := func(o, method string) {
		t.Helper()
		step("POST", o+"/pagamento", url.Values{"metodo": {method}}, http.StatusSeeOther)
	}
	addItem := func(o string) {
		t.Helper()
		step("POST", o+"/itens", url.Values{"produto": {itoa(prod)}}, http.StatusSeeOther)
	}

	// Flow 1: WhatsApp delivery, paid with PIX before the kitchen.
	o1 := create(url.Values{"canal": {"whatsapp"}, "consumo": {"entrega"}, "endereco": {"Rua A, 1"}, "nome": {"Maria"}, "telefone": {"1234"}})
	addItem(o1)
	addItem(o1)
	if msg := step("GET", o1+"/mensagem/confirmacao", nil, http.StatusOK); !strings.Contains(msg, "Olá, Maria!") || !strings.Contains(msg, "2x HOT ROLL — R$ 30,00") {
		t.Fatalf("confirmation: %q", msg)
	}
	pay(o1, "pix")
	status(o1, "cozinha")
	if ticket := step("GET", o1+"/imprimir", nil, http.StatusOK); !strings.Contains(ticket, "HOT ROLL") || !strings.Contains(ticket, "PIX") {
		t.Fatalf("ticket: %s", ticket)
	}
	status(o1, "pronto")
	status(o1, "saiu")
	step("GET", o1+"/mensagem/saiu", nil, http.StatusOK)
	status(o1, "fechado")

	// Flow 2: in-house, paid by credit card after the kitchen.
	o2 := create(url.Values{"canal": {"balcao"}, "consumo": {"local"}, "nome": {"João"}})
	addItem(o2)
	status(o2, "cozinha")
	pay(o2, "credito")
	status(o2, "pronto")
	status(o2, "fechado")

	// Flow 3: WhatsApp order eaten in, switched to local after confirmation, paid in cash.
	o3 := create(url.Values{"canal": {"whatsapp"}, "consumo": {"entrega"}, "endereco": {"Rua B, 2"}, "nome": {"Bia"}})
	addItem(o3)
	step("GET", o3+"/mensagem/confirmacao", nil, http.StatusOK)
	step("POST", o3+"/consumo", url.Values{"consumo": {"local"}}, http.StatusSeeOther)
	status(o3, "cozinha")
	status(o3, "pronto")
	step("GET", o3+"/mensagem/pronto", nil, http.StatusOK)
	// saiu does not exist for eat-in orders.
	step("POST", o3+"/status", url.Values{"para": {"saiu"}}, http.StatusConflict)
	step("POST", o3+"/pagamento", url.Values{"metodo": {"dinheiro"}, "recebido": {"20"}}, http.StatusSeeOther)
	status(o3, "fechado")

	rec := adminDo(h, s, "GET", "/admin/relatorio", nil)
	body := rec.Body.String()
	for _, want := range []string{"R$ 60,00", "4x Hot roll"} {
		if !strings.Contains(body, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

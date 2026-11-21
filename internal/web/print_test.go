package web

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/antoniohauren/ukemochi/internal/db"
)

func TestTicketLinesFit(t *testing.T) {
	o := db.Order{ID: 12, Channel: "whatsapp", Consume: db.ConsumeDelivery, ClientName: "Maria da Conceição Albuquerque Silva",
		CreatedAt: "2026-11-19T21:30:00Z", TotalCents: 123456, Notes: "Sem cebola, troco para cinquenta, interfone quebrado",
		Items: []db.OrderItem{
			{Qty: 2, UnitPriceCents: 1500, PrintableName: "HOT ROLL"},
			{Qty: 999, UnitPriceCents: 99999, PrintableName: "COMBINADO ESPECIAL DA CASA XL 40", Note: "sem wasabi"},
			{Qty: 1, UnitPriceCents: 600, PrintableName: "ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJ"},
		}}
	text := ticketText(o, false)
	for _, l := range strings.Split(text, "\n") {
		if n := utf8.RuneCountInString(l); n > ticketWidth {
			t.Errorf("line has %d chars: %q", n, l)
		}
	}
	for _, want := range []string{
		"Pedido #12           19/11 18:30", // São Paulo is UTC-3
		"HOT ROLL\n2x                      R$ 30,00\n",
		"TOTAL                R$ 1.234,56",
		"\n\n            A PAGAR",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("ticket lacks %q:\n%s", want, text)
		}
	}
	// A split bill lists each part and what is left.
	o.Payments = []db.Payment{{Method: "pix", AmountCents: 100000}, {Method: "dinheiro", AmountCents: 3456}}
	split := ticketText(o, false)
	for _, want := range []string{"PAGO PIX             R$ 1.000,00", "PAGO Dinheiro           R$ 34,56", "A PAGAR                R$ 200,00"} {
		if !strings.Contains(split, want) {
			t.Errorf("split ticket lacks %q:\n%s", want, split)
		}
	}
	o.Payments = []db.Payment{{Method: "pix", AmountCents: o.TotalCents}}
	if !strings.Contains(ticketText(o, false), "PAGO                         PIX") {
		t.Errorf("paid ticket:\n%s", ticketText(o, false))
	}
	if c := ticketText(o, true); !strings.Contains(c, "\n2x HOT ROLL\n") || strings.Contains(c, "R$ 30,00") {
		t.Errorf("kitchen ticket:\n%s", c)
	}
}

func TestWrap(t *testing.T) {
	// The indent is kept and a word longer than a line is split.
	got := wrap("  obs: " + strings.Repeat("x", 40))
	want := []string{"  obs:", strings.Repeat("x", 32), strings.Repeat("x", 8)}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("wrap = %q", got)
	}
	if got := wrap("um dois tres"); len(got) != 1 || got[0] != "um dois tres" {
		t.Errorf("short wrap = %q", got)
	}
}

func TestPrintPage(t *testing.T) {
	_, h, prod := dbServer(t)
	do(h, "POST", "/pedidos", newOrderForm("Maria"), false)
	do(h, "POST", "/pedidos/1/itens", url.Values{"produto": {itoa(prod)}}, false)
	body := do(h, "GET", "/pedidos/1/imprimir", nil, false).Body.String()
	for _, want := range []string{"/static/print.css", "window.print()", "HOT ROLL\n1x "} {
		if !strings.Contains(body, want) {
			t.Errorf("print page lacks %q", want)
		}
	}
	if k := do(h, "GET", "/pedidos/1/imprimir?cozinha=1", nil, false).Body.String(); !strings.Contains(k, "1x HOT ROLL\n") {
		t.Error("kitchen print page lacks the one-line item")
	}
	if strings.Contains(body, "app.css") {
		t.Error("print page loads Tailwind")
	}
	// The page is 58mm wide and as long as the ticket, never A4.
	if !regexp.MustCompile(`@page \{ size: 58mm \d+\.\dmm; \}`).MatchString(body) {
		t.Error("print page lacks a 58mm page size")
	}
}

func TestTicketHeight(t *testing.T) {
	// 1 line: 2.9mm + 10mm; 10 lines: 29mm + 10mm.
	if got := ticketHeight("a"); got != "12.9mm" {
		t.Errorf("1 line: %s", got)
	}
	if got := ticketHeight(strings.Repeat("a\n", 9) + "a"); got != "39.0mm" {
		t.Errorf("10 lines: %s", got)
	}
}

func TestPrintToPrinter(t *testing.T) {
	s, h, prod := dbServer(t)
	do(h, "POST", "/pedidos", newOrderForm("Maria"), false)
	do(h, "POST", "/pedidos/1/itens", url.Values{"produto": {itoa(prod)}}, true)

	// No printer: the button falls back to the browser ticket page.
	if rec := do(h, "POST", "/pedidos/1/imprimir", nil, false); rec.Header().Get("Location") != "/pedidos/1/imprimir" {
		t.Fatalf("no printer: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	var sent []string
	var fail error
	s.Print = func(text string) error { sent = append(sent, text); return fail }
	rec := do(h, "POST", "/pedidos/1/imprimir", nil, true)
	if len(sent) != 1 || !strings.Contains(sent[0], "HOT ROLL\n1x ") || !strings.Contains(rec.Body.String(), "Ticket impresso") {
		t.Fatalf("print: %q %s", sent, rec.Body)
	}
	// Sending to the kitchen prints the one-line, priceless ticket and stays on the order.
	if rec := do(h, "POST", "/pedidos/1/status", url.Values{"para": {"cozinha"}}, false); rec.Header().Get("Location") != "/pedidos/1" || len(sent) != 2 {
		t.Fatalf("kitchen: %q, %d prints", rec.Header().Get("Location"), len(sent))
	}
	if !strings.Contains(sent[1], "\n1x HOT ROLL\n") {
		t.Fatalf("kitchen ticket:\n%s", sent[1])
	}

	fail = errors.New("off")
	if rec := do(h, "POST", "/pedidos/1/imprimir", nil, true); !strings.Contains(rec.Body.String(), "Impressora não respondeu") {
		t.Fatalf("printer off: %s", rec.Body)
	}
	if rec := do(h, "POST", "/pedidos/99/imprimir", nil, false); rec.Code != http.StatusNotFound {
		t.Fatalf("missing order: %d", rec.Code)
	}
}

func TestPrintEmptyOrderRefused(t *testing.T) {
	_, h, _ := dbServer(t)
	do(h, "POST", "/pedidos", newOrderForm("Maria"), false)
	if rec := do(h, "POST", "/pedidos/1/imprimir", nil, false); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("print empty order: code %d", rec.Code)
	}
	b := do(h, "GET", "/pedidos/1", nil, false).Body.String()
	if !regexp.MustCompile(`<button[^>]*disabled[^>]*>\s*Imprimir ticket`).MatchString(b) {
		t.Error("Imprimir ticket should be disabled on an empty order")
	}
}

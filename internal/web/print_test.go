package web

import (
	"net/url"
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
	text := ticketText(o)
	for _, l := range strings.Split(text, "\n") {
		if n := utf8.RuneCountInString(l); n > ticketWidth {
			t.Errorf("line has %d chars: %q", n, l)
		}
	}
	for _, want := range []string{
		"Pedido #12           19/11 18:30", // São Paulo is UTC-3
		"2x HOT ROLL             R$ 30,00",
		"TOTAL                R$ 1.234,56",
		"A PAGAR",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("ticket lacks %q:\n%s", want, text)
		}
	}
	o.Payment = "pix"
	if !strings.Contains(ticketText(o), "PAGO                         PIX") {
		t.Errorf("paid ticket:\n%s", ticketText(o))
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
	for _, want := range []string{"/static/print.css", "window.print()", "1x HOT ROLL"} {
		if !strings.Contains(body, want) {
			t.Errorf("print page lacks %q", want)
		}
	}
	if strings.Contains(body, "app.css") {
		t.Error("print page loads Tailwind")
	}
}

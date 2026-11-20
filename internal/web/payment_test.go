package web

import (
	"context"
	"net/http"
	"net/url"
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
		typed string
		text  string
		ok    bool
	}{
		{"", "", true},
		{"50", "Troco: R$ 20,00", true},
		{"30,00", "Troco: R$ 0,00", true},
		{"20", "Faltam R$ 10,00", false},
		{"abc", "Valor inválido", false},
	} {
		if text, ok := changeText(3000, tc.typed); text != tc.text || ok != tc.ok {
			t.Errorf("changeText(3000, %q) = %q, %v; want %q, %v", tc.typed, text, ok, tc.text, tc.ok)
		}
	}
}

func TestChangeEndpoint(t *testing.T) {
	s, h, prod := dbServer(t)
	id := orderWith(t, s, prod, 2)
	rec := do(h, "GET", orderURL(id)+"/troco?valor=50", nil, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Troco: R$ 20,00") {
		t.Fatalf("code %d, body %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/pedidos/999/troco?valor=1", nil, true); !strings.Contains(rec.Body.String(), "Não encontrado") {
		t.Errorf("missing order: %s", rec.Body)
	}
}

func TestSetPayment(t *testing.T) {
	s, h, prod := dbServer(t)
	ctx := context.Background()
	id := orderWith(t, s, prod, 2) // R$ 30,00
	for _, st := range []string{db.StatusKitchen, db.StatusReady} {
		if err := s.DB.SetStatus(ctx, id, st); err != nil {
			t.Fatal(err)
		}
	}

	page := do(h, "GET", orderURL(id), nil, false).Body.String()
	if !strings.Contains(page, "Registre o pagamento para fechar") {
		t.Error("editor should explain the disabled close button")
	}

	for _, tc := range []struct {
		name string
		form url.Values
	}{
		{"no method", url.Values{}},
		{"bad method", url.Values{"metodo": {"cheque"}}},
		{"bad cash", url.Values{"metodo": {"dinheiro"}, "valor": {"12,345"}}},
		{"cash short", url.Values{"metodo": {"dinheiro"}, "valor": {"29,99"}}},
	} {
		if rec := do(h, "POST", orderURL(id)+"/pagamento", tc.form, false); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: code %d", tc.name, rec.Code)
		}
	}
	if o, _ := s.DB.GetOrder(ctx, id); o.Payment != "" {
		t.Fatalf("payment recorded after invalid input: %q", o.Payment)
	}

	rec := do(h, "POST", orderURL(id)+"/pagamento", url.Values{"metodo": {"dinheiro"}, "valor": {"50"}}, true)
	if rec.Header().Get("HX-Redirect") != orderURL(id) {
		t.Fatalf("code %d, headers %v, body %s", rec.Code, rec.Header(), rec.Body)
	}
	o, _ := s.DB.GetOrder(ctx, id)
	if o.Payment != "dinheiro" || o.CashGivenCents != 5000 {
		t.Fatalf("payment = %q, cash = %d", o.Payment, o.CashGivenCents)
	}
	page = do(h, "GET", orderURL(id), nil, false).Body.String()
	if !strings.Contains(page, "Troco R$ 20,00") || strings.Contains(page, "Registre o pagamento para fechar") {
		t.Error("editor should show the change and an enabled close button")
	}

	// Switching to card drops the cash amount.
	do(h, "POST", orderURL(id)+"/pagamento", url.Values{"metodo": {"credito"}, "valor": {"50"}}, false)
	if o, _ := s.DB.GetOrder(ctx, id); o.Payment != "credito" || o.CashGivenCents != 0 {
		t.Fatalf("payment = %q, cash = %d", o.Payment, o.CashGivenCents)
	}

	if err := s.DB.SetStatus(ctx, id, db.StatusCancelled); err != nil {
		t.Fatal(err)
	}
	if rec := do(h, "POST", orderURL(id)+"/pagamento", url.Values{"metodo": {"pix"}}, false); rec.Code != http.StatusConflict {
		t.Errorf("paying a cancelled order: code %d", rec.Code)
	}
}

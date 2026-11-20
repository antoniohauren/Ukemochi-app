package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/antoniohauren/ukemochi/internal/db"
)

// dbServer returns a handler over a temp DB holding one active product (Hot roll, R$ 15,00).
func dbServer(t *testing.T) (*Server, http.Handler, int64) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	ctx := context.Background()
	cat, err := d.CreateCategory(ctx, "Sushi", 0)
	if err != nil {
		t.Fatal(err)
	}
	item, err := d.CreateItem(ctx, db.Item{CategoryID: cat, Name: "Hot roll", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	prod, err := d.CreateProduct(ctx, db.Product{ItemID: item, PrintableName: "HOT ROLL", PriceCents: 1500, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: d, Auth: &Auth{PIN: "4321", Key: []byte("0123456789abcdef0123456789abcdef")}}
	mux := http.NewServeMux()
	s.Routes(mux)
	return s, mux, prod
}

func do(h http.Handler, method, target string, form url.Values, hx bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newOrderForm(name string) url.Values {
	return url.Values{"canal": {"whatsapp"}, "consumo": {"entrega"}, "endereco": {"Rua A, 1"}, "nome": {name}, "telefone": {"1234"}}
}

func TestCreateOrderValidation(t *testing.T) {
	_, h, _ := dbServer(t)
	for _, tc := range []struct {
		name string
		form url.Values
	}{
		{"no name", url.Values{"canal": {"whatsapp"}, "nome": {"  "}}},
		{"bad channel", url.Values{"canal": {"ifood"}, "nome": {"Maria"}}},
		{"bad consume", url.Values{"canal": {"whatsapp"}, "consumo": {"x"}, "nome": {"Maria"}}},
		{"entrega without address", url.Values{"canal": {"whatsapp"}, "consumo": {"entrega"}, "nome": {"Maria"}}},
		{"long address", url.Values{"canal": {"whatsapp"}, "consumo": {"entrega"}, "endereco": {strings.Repeat("a", 121)}, "nome": {"Maria"}}},
		{"bad phone", url.Values{"canal": {"whatsapp"}, "nome": {"Maria"}, "telefone": {"12a4"}}},
		{"long name", url.Values{"canal": {"whatsapp"}, "nome": {strings.Repeat("a", 61)}}},
	} {
		if rec := do(h, "POST", "/pedidos", tc.form, false); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: code %d", tc.name, rec.Code)
		}
	}
}

func TestOrderFlow(t *testing.T) {
	s, h, prod := dbServer(t)
	ctx := context.Background()

	rec := do(h, "POST", "/pedidos", newOrderForm("Maria"), false)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/pedidos/1" {
		t.Fatalf("create: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// Sending an empty order to the kitchen is refused.
	if rec := do(h, "POST", "/pedidos/1/status", url.Values{"para": {"cozinha"}}, false); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty to kitchen: %d", rec.Code)
	}

	rec = do(h, "POST", "/pedidos/1/itens", url.Values{"produto": {itoa(prod)}}, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "R$ 15,00") || !strings.Contains(rec.Body.String(), `id="acoes"`) {
		t.Fatalf("add item: %d %s", rec.Code, rec.Body)
	}
	do(h, "POST", "/pedidos/1/itens", url.Values{"produto": {itoa(prod)}}, true)
	o, _ := s.DB.GetOrder(ctx, 1)
	if len(o.Items) != 1 || o.Items[0].Qty != 2 || o.TotalCents != 3000 {
		t.Fatalf("after adds: %+v", o)
	}
	line := "/pedidos/1/itens/" + itoa(o.Items[0].ID)

	for _, q := range []string{"-1", "1000", "x"} {
		req := httptest.NewRequest("PATCH", line, strings.NewReader("qty="+q))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Target", "pedido-itens")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		// The error shows in the toast and, since the sheet covers the toast, in the sheet too.
		if b := rec.Body.String(); strings.Count(b, "Quantidade") != 2 || !strings.Contains(b, `id="sheet-error"`) {
			t.Errorf("qty %q: %s", q, b)
		}
	}
	if rec := do(h, "PATCH", line, url.Values{"qty": {"3"}}, true); rec.Code != http.StatusOK {
		t.Fatalf("patch: %d", rec.Code)
	}
	// Without htmx the stepper form posts and is redirected back to the editor.
	if rec := do(h, "POST", line, url.Values{"qty": {"3"}}, false); rec.Code != http.StatusSeeOther {
		t.Fatalf("post fallback: %d", rec.Code)
	}

	msg := do(h, "GET", "/pedidos/1/mensagem/confirmacao", nil, false).Body.String()
	if want := "3x Hot roll — R$ 45,00\n\n*Total: R$ 45,00*"; !strings.Contains(msg, want) {
		t.Fatalf("message %q lacks %q", msg, want)
	}
	if rec := do(h, "GET", "/pedidos/1/mensagem/outra", nil, false); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown message: %d", rec.Code)
	}

	rec = do(h, "POST", "/pedidos/1/status", url.Values{"para": {"cozinha"}}, false)
	if rec.Header().Get("Location") != "/pedidos/1/imprimir" {
		t.Fatalf("to kitchen: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// Items are locked once in the kitchen; htmx gets a toast instead of a swap.
	rec = do(h, "PATCH", line, url.Values{"qty": {"1"}}, true)
	if rec.Header().Get("HX-Reswap") != "none" || !strings.Contains(rec.Body.String(), "pedido aberto") {
		t.Fatalf("edit in kitchen: %s", rec.Body)
	}
	// pronto -> saiu is fine for delivery; skipping straight to fechado is not.
	if rec := do(h, "POST", "/pedidos/1/status", url.Values{"para": {"fechado"}}, false); rec.Code != http.StatusConflict {
		t.Fatalf("kitchen to closed: %d", rec.Code)
	}
	do(h, "POST", "/pedidos/1/status", url.Values{"para": {"pronto"}}, false)
	do(h, "POST", "/pedidos/1/status", url.Values{"para": {"saiu"}}, false)
	// Closing unpaid fails on the server.
	if rec := do(h, "POST", "/pedidos/1/status", url.Values{"para": {"fechado"}}, false); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "pagamento") {
		t.Fatalf("close unpaid: %d", rec.Code)
	}

	page := do(h, "GET", "/", nil, false).Body.String()
	if !strings.Contains(page, "Maria") || !strings.Contains(page, "Saiu p/ entrega") {
		t.Fatalf("list lacks the order")
	}

	// Cancel, reopen, then the line can be deleted again.
	// A delivery can be cancelled after the courier left: the client cancelled or was not home.
	if rec := do(h, "POST", "/pedidos/1/status", url.Values{"para": {"cancelado"}}, false); rec.Code != http.StatusSeeOther {
		t.Fatalf("saiu to cancelado: %d", rec.Code)
	}
	do(h, "POST", "/pedidos", newOrderForm("Bia"), false)
	for _, to := range []string{"cancelado", "aberto"} {
		if rec := do(h, "POST", "/pedidos/2/status", url.Values{"para": {to}}, false); rec.Code != http.StatusSeeOther {
			t.Fatalf("to %s: %d", to, rec.Code)
		}
	}
	do(h, "POST", "/pedidos/2/itens", url.Values{"produto": {itoa(prod)}}, false)
	o2, _ := s.DB.GetOrder(ctx, 2)
	if rec := do(h, "DELETE", "/pedidos/2/itens/"+itoa(o2.Items[0].ID), nil, true); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	if o2, _ = s.DB.GetOrder(ctx, 2); len(o2.Items) != 0 {
		t.Fatalf("line not deleted: %+v", o2.Items)
	}
}

func TestConsumeAndSaiu(t *testing.T) {
	s, h, prod := dbServer(t)
	do(h, "POST", "/pedidos", newOrderForm("Ana"), false)
	if rec := do(h, "POST", "/pedidos/1/consumo", url.Values{"consumo": {"local"}}, false); rec.Code != http.StatusSeeOther {
		t.Fatalf("consume: %d", rec.Code)
	}
	if rec := do(h, "POST", "/pedidos/1/consumo", url.Values{"consumo": {"drive"}}, false); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad consume: %d", rec.Code)
	}
	// Back to entrega needs an address; switching to local kept the old one, so the form shows it again.
	if rec := do(h, "POST", "/pedidos/1/consumo", url.Values{"consumo": {"entrega"}, "endereco": {" "}}, false); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("entrega without address: %d", rec.Code)
	}
	if o, _ := s.DB.GetOrder(context.Background(), 1); o.Address != "Rua A, 1" {
		t.Fatalf("address after local = %q", o.Address)
	}
	if rec := do(h, "POST", "/pedidos/1/consumo", url.Values{"consumo": {"entrega"}, "endereco": {"Rua Nova, 10"}}, false); rec.Code != http.StatusSeeOther {
		t.Fatalf("entrega with address: %d", rec.Code)
	}
	if body := do(h, "GET", "/pedidos/1/imprimir", nil, false).Body.String(); !strings.Contains(body, "Rua Nova, 10") {
		t.Error("ticket should print the delivery address")
	}
	// The delivery settings live in their own sheet, opened from the actions sheet, with a Voltar.
	page := do(h, "GET", "/pedidos/1", nil, false).Body.String()
	if !strings.Contains(page, `id="consumo-sheet"`) || !strings.Contains(page, "consumo-sheet&#39;).showModal()") ||
		!strings.Contains(page, "document.getElementById(&#39;consumo-sheet&#39;).close(); document.getElementById(&#39;acoes-sheet&#39;).showModal()") {
		t.Error("delivery settings should live in their own sheet with a Voltar")
	}
	if strings.Count(page, `name="endereco"`) != 1 {
		t.Error("the address field should only be in the delivery sheet")
	}
	// With htmx the delivery sheet is swapped in place, and the actions sheet and header follow out of band.
	rec := do(h, "POST", "/pedidos/1/consumo", url.Values{"consumo": {"local"}}, true)
	if b := rec.Body.String(); rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "" || rec.Header().Get("HX-Trigger") != "consumo-salvo" || !strings.HasPrefix(b, `<div id="consumo"`) ||
		!strings.Contains(b, `id="acoes" hx-swap-oob="true"`) || !strings.Contains(b, `id="pedido-cabecalho" hx-swap-oob="true"`) {
		t.Fatalf("htmx consume: %d %v %s", rec.Code, rec.Header(), b)
	}
	ctx := context.Background()
	if err := s.DB.AddItem(ctx, 1, prod, 1, ""); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{db.StatusKitchen, db.StatusReady} {
		if err := s.DB.SetStatus(ctx, 1, st); err != nil {
			t.Fatal(err)
		}
	}
	if rec := do(h, "POST", "/pedidos/1/status", url.Values{"para": {"saiu"}}, false); rec.Code != http.StatusConflict {
		t.Fatalf("saiu for local: %d", rec.Code)
	}
}

func TestBadIDs(t *testing.T) {
	_, h, _ := dbServer(t)
	for _, target := range []string{"/pedidos/0", "/pedidos/abc", "/pedidos/99", "/pedidos/99/imprimir"} {
		if rec := do(h, "GET", target, nil, false); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", target, rec.Code)
		}
	}
	if rec := do(h, "POST", "/pedidos/99/itens", url.Values{"produto": {"1"}}, false); rec.Code != http.StatusNotFound {
		t.Errorf("add to missing order: %d", rec.Code)
	}
}

func TestMessages(t *testing.T) {
	o := db.Order{ClientName: "Maria", Channel: "whatsapp", Consume: db.ConsumeLocal, Status: db.StatusReady, TotalCents: 3600,
		Items: []db.OrderItem{{Qty: 2, UnitPriceCents: 1500, NameSnapshot: "Hot roll"}, {Qty: 1, UnitPriceCents: 600, NameSnapshot: "Coca-Cola Zero Lata"}}}
	got, _ := orderMessage(o, msgConfirm)
	want := "Olá, Maria! Confirmando seu pedido:\n\n2x Hot roll — R$ 30,00\n1x Coca-Cola Zero Lata — R$ 6,00\n\n*Total: R$ 36,00*"
	if got != want {
		t.Errorf("confirm:\n%s\nwant:\n%s", got, want)
	}
	if got, _ := orderMessage(o, msgReady); got != "Maria, seu pedido está pronto! 🍣" {
		t.Errorf("ready: %q", got)
	}
	if got, _ := orderMessage(o, msgOut); got != "Maria, seu pedido saiu para entrega! 🛵" {
		t.Errorf("out: %q", got)
	}
	if n := len(copyMessages(o)); n != 2 {
		t.Errorf("copy messages for ready local: %d", n)
	}
	o.Channel = "balcao"
	if n := len(copyMessages(o)); n != 0 {
		t.Errorf("balcão gets messages: %d", n)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestBalcaoIsLocal(t *testing.T) {
	s, h, _ := dbServer(t)
	// The form always posts a consumo value, entrega by default.
	do(h, "POST", "/pedidos", url.Values{"canal": {"balcao"}, "consumo": {"entrega"}, "nome": {"João"}}, false)
	if o, _ := s.DB.GetOrder(context.Background(), 1); o.Consume != db.ConsumeLocal {
		t.Fatalf("consume = %q", o.Consume)
	}
}

func TestAddItemSteps(t *testing.T) {
	s, h, prod := dbServer(t)
	do(h, "POST", "/pedidos", newOrderForm("Maria"), false)

	// The editor offers the add sheet, opened by the button.
	if body := do(h, "GET", "/pedidos/1", nil, false).Body.String(); !strings.Contains(body, `id="add-sheet"`) ||
		!strings.Contains(body, `showModal()`) || !strings.Contains(body, "?categoria=1") {
		t.Fatalf("editor: %s", body)
	}
	// Step 1 lists categories; step 2 the items; step 3 the item's variants; step 4 the quantity form.
	// An item with one product links straight to quantity.
	ctx := context.Background()
	solo, _ := s.DB.CreateItem(ctx, db.Item{CategoryID: 1, Name: "Guioza", Active: true})
	soloProd, _ := s.DB.CreateProduct(ctx, db.Product{ItemID: solo, PrintableName: "GUIOZA", PriceCents: 2000, Active: true})
	if _, err := s.DB.CreateProduct(ctx, db.Product{ItemID: 1, Size: "Todo (8un)", PrintableName: "HOT TODO", PriceCents: 2800, Active: true}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ target, want string }{
		{"/pedidos/1/adicionar", "Sushi"},
		{"/pedidos/1/adicionar?categoria=1", "?item=1"},
		{"/pedidos/1/adicionar?categoria=1", "?produto=" + itoa(soloProd)},
		{"/pedidos/1/adicionar?item=1", "Todo (8un)"},
		{"/pedidos/1/adicionar?item=1", "?produto=" + itoa(prod)},
		{"/pedidos/1/adicionar?produto=" + itoa(prod), `name="qty"`},
	} {
		for _, hx := range []bool{true, false} {
			rec := do(h, "GET", tc.target, nil, hx)
			body := rec.Body.String()
			if rec.Code != http.StatusOK || !strings.Contains(body, tc.want) || strings.Contains(body, "<html") == hx {
				t.Errorf("%s hx=%v: %d %s", tc.target, hx, rec.Code, body)
			}
		}
	}
	// htmx steps rename the sheet to the current pick and offer a way back.
	if body := do(h, "GET", "/pedidos/1/adicionar?item=1", nil, true).Body.String(); !strings.Contains(body, `id="add-sheet-titulo" hx-swap-oob="true"`) ||
		!strings.Contains(body, "Voltar") || !strings.Contains(body, "?categoria=1") {
		t.Errorf("step title: %s", body)
	}
	for _, target := range []string{"/pedidos/1/adicionar?categoria=99", "/pedidos/1/adicionar?item=99", "/pedidos/1/adicionar?produto=99", "/pedidos/1/adicionar?produto=x", "/pedidos/99/adicionar"} {
		if rec := do(h, "GET", target, nil, false); rec.Code != http.StatusNotFound {
			t.Errorf("%s: code %d", target, rec.Code)
		}
	}

	// Quantity is validated, then added in one line; htmx gets the close-sheet trigger.
	for _, q := range []string{"0", "-1", "1000", "x"} {
		if rec := do(h, "POST", "/pedidos/1/itens", url.Values{"produto": {itoa(prod)}, "qty": {q}}, false); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("qty %q: code %d", q, rec.Code)
		}
	}
	rec := do(h, "POST", "/pedidos/1/itens", url.Values{"produto": {itoa(prod)}, "qty": {"3"}}, true)
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Trigger") != "item-added" {
		t.Fatalf("add qty: %d %v", rec.Code, rec.Header())
	}
	o, _ := s.DB.GetOrder(ctx, 1)
	if len(o.Items) != 1 || o.Items[0].Qty != 3 || o.TotalCents != 4500 {
		t.Fatalf("after add: %+v", o)
	}

	// A closed-for-edits order sends the steps back to the editor.
	do(h, "POST", "/pedidos/1/status", url.Values{"para": {"cozinha"}}, false)
	if rec := do(h, "GET", "/pedidos/1/adicionar", nil, false); rec.Code != http.StatusSeeOther {
		t.Errorf("not open: code %d", rec.Code)
	}
}

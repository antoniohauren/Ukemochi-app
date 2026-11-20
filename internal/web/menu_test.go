package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func adminDo(h http.Handler, s *Server, method, target string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: s.Auth.newSession()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMenuNeedsAdmin(t *testing.T) {
	_, h, _ := dbServer(t)
	for _, method := range []string{"GET", "POST"} {
		target := "/admin/cardapio"
		if method == "POST" {
			target += "/categorias"
		}
		rec := do(h, method, target, url.Values{"nome": {"X"}}, false)
		if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/login") {
			t.Errorf("%s %s without session: %d", method, target, rec.Code)
		}
	}
}

func TestMenuCRUD(t *testing.T) {
	s, h, prod := dbServer(t)
	ctx := context.Background()

	if rec := adminDo(h, s, "POST", "/admin/cardapio/categorias", url.Values{"nome": {"Bebidas"}, "posicao": {"2"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create category: %d %s", rec.Code, rec.Body)
	}
	if rec := adminDo(h, s, "POST", "/admin/cardapio/categorias", url.Values{"nome": {"Bebidas"}}); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "Já existe") {
		t.Fatalf("duplicate category: %d", rec.Code)
	}
	if rec := adminDo(h, s, "POST", "/admin/cardapio/itens", url.Values{"categoria": {"99"}, "nome": {"Água"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("item in missing category: %d", rec.Code)
	}
	if rec := adminDo(h, s, "POST", "/admin/cardapio/itens", url.Values{"categoria": {"2"}, "nome": {"Água"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create item: %d %s", rec.Code, rec.Body)
	}
	for _, f := range []url.Values{
		{"item": {"2"}, "impresso": {"AGUA"}, "preco": {"abc"}},
		{"item": {"2"}, "impresso": {""}, "preco": {"4,00"}},
		{"item": {"2"}, "impresso": {strings.Repeat("A", ticketWidth+1)}, "preco": {"4,00"}},
		{"item": {"77"}, "impresso": {"AGUA"}, "preco": {"4,00"}},
	} {
		if rec := adminDo(h, s, "POST", "/admin/cardapio/produtos", f); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("bad product %v: %d", f, rec.Code)
		}
	}
	if rec := adminDo(h, s, "POST", "/admin/cardapio/produtos", url.Values{"item": {"2"}, "impresso": {"AGUA"}, "preco": {"4,50"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create product: %d %s", rec.Code, rec.Body)
	}

	// Saving without "ativo" deactivates the product and hides it from the order menu.
	rec := adminDo(h, s, "POST", "/admin/cardapio/produtos/"+itoa(prod), url.Values{"impresso": {"HOT ROLL"}, "preco": {"18,00"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/cardapio#item-1" {
		t.Fatalf("update product: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	menu, _ := s.DB.Menu(ctx, true)
	for _, c := range menu {
		for _, it := range c.Items {
			if it.Name == "Hot roll" {
				t.Fatalf("inactive product still on the menu")
			}
		}
	}
	if rec := adminDo(h, s, "POST", "/admin/cardapio/produtos/99", url.Values{"impresso": {"X"}, "preco": {"1"}}); rec.Code != http.StatusNotFound {
		t.Fatalf("update missing product: %d", rec.Code)
	}

	page := adminDo(h, s, "GET", "/admin/cardapio", nil).Body.String()
	for _, want := range []string{"Bebidas", "Água", "AGUA", `value="4,50"`} {
		if !strings.Contains(page, want) {
			t.Errorf("menu page lacks %q", want)
		}
	}
}

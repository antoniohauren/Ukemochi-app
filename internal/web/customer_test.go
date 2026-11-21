package web

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/antoniohauren/ukemochi/internal/db"
)

func TestSetCustomer(t *testing.T) {
	s, h, prod := dbServer(t)
	ctx := context.Background()
	id := orderWith(t, s, prod, 1)
	u := orderURL(id) + "/cliente"

	rec := hxDo(h, "POST", u, url.Values{"nome": {" João "}, "telefone": {"9876"}, "obs": {"sem pressa"}}, "cliente")
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Trigger") != "cliente-salvo" {
		t.Fatalf("edit: %d %v", rec.Code, rec.Header())
	}
	o, _ := s.DB.GetOrder(ctx, id)
	if o.ClientName != "João" || o.PhoneLast4 != "9876" || o.Notes != "sem pressa" {
		t.Fatalf("after edit: %+v", o)
	}
	// Clearing the phone stores none.
	if rec := do(h, "POST", u, url.Values{"nome": {"João"}}, false); rec.Code != http.StatusSeeOther {
		t.Fatalf("no-JS edit: %d", rec.Code)
	}
	if o, _ := s.DB.GetOrder(ctx, id); o.PhoneLast4 != "" || o.Notes != "" {
		t.Fatalf("after clearing: %+v", o)
	}

	for name, form := range map[string]url.Values{
		"empty name": {"nome": {"  "}},
		"bad phone":  {"nome": {"Ana"}, "telefone": {"12a4"}},
	} {
		if rec := do(h, "POST", u, form, false); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: code %d", name, rec.Code)
		}
	}
	if o, _ := s.DB.GetOrder(ctx, id); o.ClientName != "João" {
		t.Fatalf("invalid edit changed the name: %q", o.ClientName)
	}

	if err := s.DB.AddPayment(ctx, id, "pix", 1500, 0, 1); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{db.StatusKitchen, db.StatusReady, db.StatusClosed} {
		if err := s.DB.SetStatus(ctx, id, st); err != nil {
			t.Fatal(err)
		}
	}
	if rec := do(h, "POST", u, url.Values{"nome": {"Ana"}}, false); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("fechado: code %d", rec.Code)
	}
	if err := s.DB.SetCustomer(ctx, id, "Ana", "", ""); err != db.ErrBadTransition {
		t.Errorf("db on fechado: %v", err)
	}
}

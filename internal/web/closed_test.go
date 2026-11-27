package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/antoniohauren/ukemochi/internal/db"
)

func TestClosedOrders(t *testing.T) {
	s, h, prod := dbServer(t)
	ctx := context.Background()
	open := orderWith(t, s, prod, 1) // stays aberto
	paid := orderWith(t, s, prod, 1)
	if err := s.DB.AddPayment(ctx, paid, "pix", 1500, 0, 1); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{db.StatusReady, db.StatusClosed} {
		if err := s.DB.SetStatus(ctx, paid, st); err != nil {
			t.Fatal(err)
		}
	}
	cancelled := orderWith(t, s, prod, 1)
	if err := s.DB.SetStatus(ctx, cancelled, db.StatusCancelled); err != nil {
		t.Fatal(err)
	}

	card := func(id int64) string { return `href="/pedidos/` + itoa(id) + `"` }
	b := do(h, "GET", "/pedidos/fechados", nil, false).Body.String()
	if !strings.Contains(b, card(paid)) || !strings.Contains(b, card(cancelled)) || strings.Contains(b, card(open)) {
		t.Errorf("today's closed list: %s", b)
	}
	// Newest first: the cancelled order (created last, no closed_at) is above the one closed earlier.
	if strings.Index(b, card(cancelled)) > strings.Index(b, card(paid)) {
		t.Error("closed list should be newest first")
	}
	if !strings.Contains(b, "2 pedidos") || !strings.Contains(b, `aria-current="page"`) {
		t.Error("count or active nav item missing")
	}

	yesterday := time.Now().In(saoPaulo).AddDate(0, 0, -1).Format(dayLayout)
	if b := do(h, "GET", "/pedidos/fechados?dia="+yesterday, nil, false).Body.String(); !strings.Contains(b, "Nenhum pedido fechado neste dia.") {
		t.Error("yesterday should be empty")
	}
	if rec := do(h, "GET", "/pedidos/fechados?dia=ontem", nil, false); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad day: code %d", rec.Code)
	}
	// Reopening takes it off the closed list.
	if err := s.DB.SetStatus(ctx, paid, db.StatusOpen); err != nil {
		t.Fatal(err)
	}
	if b := do(h, "GET", "/pedidos/fechados", nil, false).Body.String(); strings.Contains(b, card(paid)) {
		t.Error("reopened order still listed as closed")
	}
}

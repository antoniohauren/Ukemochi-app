package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/antoniohauren/ukemochi/internal/db"
)

func TestReportDay(t *testing.T) {
	// 01:30 UTC on Nov 20 is still Nov 19 in São Paulo (UTC-3).
	now := time.Date(2026, 11, 20, 1, 30, 0, 0, time.UTC)
	day, err := reportDay("", now)
	if err != nil || day.Format(dayLayout) != "2026-11-19" {
		t.Fatalf("default day = %v, %v", day, err)
	}
	if got := day.UTC().Format(time.RFC3339); got != "2026-11-19T03:00:00Z" {
		t.Errorf("day start in UTC = %s", got)
	}
	for _, bad := range []string{"19/11/2026", "2026-13-01", "x"} {
		if _, err := reportDay(bad, now); err == nil {
			t.Errorf("reportDay(%q) accepted", bad)
		}
	}
}

func TestReportSummary(t *testing.T) {
	day := time.Date(2026, 11, 19, 0, 0, 0, 0, saoPaulo)
	got := reportSummary(day, db.Report{
		Orders: 3, TotalCents: 4500,
		ByPayment: []db.ReportRow{{Key: "pix", Orders: 2, TotalCents: 3000}, {Key: "dinheiro", Orders: 1, TotalCents: 1500}},
		ByChannel: []db.ReportRow{{Key: "balcao", Orders: 3, TotalCents: 4500}},
		TopItems:  []db.TopItem{{Name: "Hot roll", Qty: 3, TotalCents: 4500}},
	})
	want := "*Resumo 19/11/2026*\nPedidos: 3 · Total: R$ 45,00\n\n" +
		"*Pagamento*\nPIX: 2 · R$ 30,00\nDinheiro: 1 · R$ 15,00\n\n" +
		"*Canal*\nBalcão: 3 · R$ 45,00\n\n" +
		"*Mais vendidos*\n3x Hot roll"
	if got != want {
		t.Errorf("summary:\n%s\nwant:\n%s", got, want)
	}
}

func TestReportPage(t *testing.T) {
	s, h, prod := dbServer(t)
	ctx := context.Background()

	if rec := do(h, "GET", "/admin/relatorio", nil, false); rec.Code != http.StatusSeeOther {
		t.Fatalf("without session: code %d", rec.Code)
	}

	id := orderWith(t, s, prod, 3)
	if err := s.DB.SetPayment(ctx, id, "pix", 0); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{db.StatusKitchen, db.StatusReady, db.StatusClosed} {
		if err := s.DB.SetStatus(ctx, id, st); err != nil {
			t.Fatal(err)
		}
	}

	rec := adminDo(h, s, "GET", "/admin/relatorio", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "R$ 45,00") || !strings.Contains(body, "3x Hot roll") {
		t.Fatalf("code %d, body %s", rec.Code, body)
	}
	if rec := adminDo(h, s, "GET", "/admin/relatorio?dia=2000-01-01", nil); !strings.Contains(rec.Body.String(), "Nenhum pedido fechado") {
		t.Error("old day should be empty")
	}
	if rec := adminDo(h, s, "GET", "/admin/relatorio?dia=ontem", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad day: code %d", rec.Code)
	}
}

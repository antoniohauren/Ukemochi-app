package web

import (
	"context"
	"net/http"
	"net/url"
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
	}, cashBox{OpeningCents: 20000, ReceivedCents: 1500})
	want := "*Resumo 19/11/2026*\nPedidos: 3 · Total: R$ 45,00\n\n" +
		"*Pagamento*\nPIX: 2 · R$ 30,00\nDinheiro: 1 · R$ 15,00\n\n" +
		"*Caixa*\nInicial: R$ 200,00\nDinheiro recebido: R$ 15,00\nEsperado: R$ 215,00\n\n" +
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
	if err := s.DB.AddPayment(ctx, id, "pix", 4500, 0, 1); err != nil {
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
	if !strings.Contains(body, ">1 pedido<") {
		t.Error("one order should read \"1 pedido\"")
	}
	if rec := adminDo(h, s, "GET", "/admin/relatorio?dia=2000-01-01", nil); !strings.Contains(rec.Body.String(), "Nenhum pedido fechado") {
		t.Error("old day should be empty")
	}
	if rec := adminDo(h, s, "GET", "/admin/relatorio?dia=ontem", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad day: code %d", rec.Code)
	}
}

func TestLoginPageLinksHome(t *testing.T) {
	_, h, _ := dbServer(t)
	if b := do(h, "GET", "/admin/login", nil, false).Body.String(); !strings.Contains(b, `href="/"`) || !strings.Contains(b, "← Pedidos") {
		t.Errorf("login page lacks the link home: %s", b)
	}
}

func TestOpeningCash(t *testing.T) {
	s, h, prod := dbServer(t)
	ctx := context.Background()
	today := time.Now().In(saoPaulo).Format(dayLayout)

	// R$ 45,00 split: R$ 30,00 PIX, R$ 15,00 cash. Only the cash counts toward the drawer.
	id := orderWith(t, s, prod, 3)
	if err := s.DB.AddPayment(ctx, id, "pix", 3000, 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.AddPayment(ctx, id, "dinheiro", 1500, 2000, 1); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{db.StatusKitchen, db.StatusReady, db.StatusClosed} {
		if err := s.DB.SetStatus(ctx, id, st); err != nil {
			t.Fatal(err)
		}
	}

	if rec := do(h, "POST", "/admin/relatorio/caixa", url.Values{"dia": {today}, "caixa_inicial": {"200"}}, false); rec.Code != http.StatusSeeOther ||
		strings.HasPrefix(rec.Header().Get("Location"), "/admin/relatorio") {
		t.Fatalf("without session: code %d, location %q", rec.Code, rec.Header().Get("Location"))
	}
	rec := adminDo(h, s, "POST", "/admin/relatorio/caixa", url.Values{"dia": {today}, "caixa_inicial": {"200,00"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/relatorio?dia="+today {
		t.Fatalf("set: code %d, location %q", rec.Code, rec.Header().Get("Location"))
	}
	body := adminDo(h, s, "GET", "/admin/relatorio?dia="+today, nil).Body.String()
	if !strings.Contains(body, "Caixa esperado") || !strings.Contains(body, "R$ 215,00") || !strings.Contains(body, `value="200,00"`) {
		t.Errorf("report should show R$ 200,00 + R$ 15,00 = R$ 215,00, body %s", body)
	}

	// Another day starts from zero.
	if body := adminDo(h, s, "GET", "/admin/relatorio?dia=2000-01-01", nil).Body.String(); !strings.Contains(body, "Caixa esperado") || strings.Contains(body, "R$ 215,00") {
		t.Error("an old day should have its own, empty drawer")
	}

	for _, bad := range []url.Values{
		{"dia": {today}, "caixa_inicial": {"-5"}},
		{"dia": {today}, "caixa_inicial": {"abc"}},
		{"dia": {"ontem"}, "caixa_inicial": {"10"}},
	} {
		if rec := adminDo(h, s, "POST", "/admin/relatorio/caixa", bad); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%v: code %d", bad, rec.Code)
		}
	}
}

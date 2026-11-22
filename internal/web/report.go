package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/antoniohauren/ukemochi/internal/db"
	"github.com/antoniohauren/ukemochi/internal/money"
)

const dayLayout = "2006-01-02"

func (s *Server) registerReport(mux *http.ServeMux) {
	mux.Handle("GET /admin/relatorio", s.requireAdmin(http.HandlerFunc(s.report)))
	mux.Handle("POST /admin/relatorio/caixa", s.requireAdmin(http.HandlerFunc(s.setOpeningCash)))
}

// cashBox is the day's drawer: what it opened with and the cash received on closed orders.
type cashBox struct {
	OpeningCents  int64
	ReceivedCents int64
}

// ExpectedCents is what should be in the drawer: the opening cash plus the cash received.
func (c cashBox) ExpectedCents() int64 { return c.OpeningCents + c.ReceivedCents }

func newCashBox(opening int64, rep db.Report) cashBox {
	c := cashBox{OpeningCents: opening}
	for _, row := range rep.ByPayment {
		if row.Key == "dinheiro" {
			c.ReceivedCents = row.TotalCents
		}
	}
	return c
}

func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	day, err := reportDay(r.URL.Query().Get("dia"), time.Now())
	if err != nil {
		fail(w, r, err)
		return
	}
	// AddDate keeps local midnight, so days with a DST change still get the right bounds.
	rep, err := s.DB.DailyReport(r.Context(), day, day.AddDate(0, 0, 1))
	if err != nil {
		fail(w, r, err)
		return
	}
	opening, err := s.DB.OpeningCash(r.Context(), day.Format(dayLayout))
	if err != nil {
		fail(w, r, err)
		return
	}
	cash := newCashBox(opening, rep)
	render(w, r, http.StatusOK, ReportPage(day, rep, cash, reportSummary(day, rep, cash)))
}

// setOpeningCash saves one day's caixa inicial and goes back to that day's report. Empty means 0.
func (s *Server) setOpeningCash(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		fail(w, r, err)
		return
	}
	day, err := reportDay(r.PostFormValue("dia"), time.Now())
	if err != nil {
		fail(w, r, err)
		return
	}
	var cents int64
	if v := strings.TrimSpace(r.PostFormValue("caixa_inicial")); v != "" {
		if cents, err = money.Parse(v); err != nil {
			fail(w, r, inputError("Caixa inicial inválido. Ex.: 200,00"))
			return
		}
	}
	if err := s.DB.SetOpeningCash(r.Context(), day.Format(dayLayout), cents); err != nil {
		fail(w, r, err)
		return
	}
	redirect(w, r, "/admin/relatorio?dia="+day.Format(dayLayout))
}

// reportDay parses dia (YYYY-MM-DD) as local midnight in São Paulo; empty means the day of now.
func reportDay(dia string, now time.Time) (time.Time, error) {
	if dia == "" {
		dia = now.In(saoPaulo).Format(dayLayout)
	}
	day, err := time.ParseInLocation(dayLayout, dia, saoPaulo)
	if err != nil {
		return day, inputError("Data inválida.")
	}
	return day, nil
}

func label(labels map[string]string, key string) string {
	if l, ok := labels[key]; ok {
		return l
	}
	return key
}

// reportSummary is the WhatsApp text behind Copiar resumo.
func reportSummary(day time.Time, rep db.Report, cash cashBox) string {
	var b strings.Builder
	fmt.Fprintf(&b, "*Resumo %s*\n", day.Format("02/01/2006"))
	fmt.Fprintf(&b, "Pedidos: %d · Total: %s\n", rep.Orders, money.Format(rep.TotalCents))
	section := func(title string, rows []db.ReportRow, labels map[string]string) {
		if len(rows) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n*%s*\n", title)
		for _, row := range rows {
			fmt.Fprintf(&b, "%s: %d · %s\n", label(labels, row.Key), row.Orders, money.Format(row.TotalCents))
		}
	}
	section("Pagamento", rep.ByPayment, paymentLabels)
	if cash.OpeningCents > 0 || cash.ReceivedCents > 0 {
		fmt.Fprintf(&b, "\n*Caixa*\nInicial: %s\nDinheiro recebido: %s\nEsperado: %s\n",
			money.Format(cash.OpeningCents), money.Format(cash.ReceivedCents), money.Format(cash.ExpectedCents()))
	}
	section("Canal", rep.ByChannel, channelLabels)
	if len(rep.TopItems) > 0 {
		b.WriteString("\n*Mais vendidos*\n")
		for _, it := range rep.TopItems {
			fmt.Fprintf(&b, "%dx %s\n", it.Qty, it.Name)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

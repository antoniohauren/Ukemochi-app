package web

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/antoniohauren/ukemochi/internal/db"
	"github.com/antoniohauren/ukemochi/internal/money"
)

// ticketWidth is the characters per line on 58mm paper in monospace.
const ticketWidth = 32

var paymentLabels = map[string]string{"pix": "PIX", "credito": "Crédito", "debito": "Débito", "dinheiro": "Dinheiro"}

func (s *Server) registerPrint(mux *http.ServeMux) {
	mux.HandleFunc("GET /pedidos/{id}/imprimir", func(w http.ResponseWriter, r *http.Request) {
		o, ok := s.loadOrder(w, r)
		if !ok {
			return
		}
		render(w, r, http.StatusOK, TicketPage(o, ticketText(o)))
	})
	// Without a configured printer this falls back to the browser ticket page.
	mux.HandleFunc("POST /pedidos/{id}/imprimir", func(w http.ResponseWriter, r *http.Request) {
		o, ok := s.loadOrder(w, r)
		if !ok {
			return
		}
		if len(o.Items) == 0 {
			fail(w, r, inputError("Adicione itens antes de imprimir."))
			return
		}
		if s.Print == nil {
			redirect(w, r, orderURL(o.ID)+"/imprimir")
			return
		}
		if err := s.printTicket(o); err != nil {
			fail(w, r, inputError("Impressora não respondeu. Ligue a impressora e tente de novo."))
			return
		}
		if !isHX(r) {
			http.Redirect(w, r, orderURL(o.ID), http.StatusSeeOther)
			return
		}
		w.Header().Set("HX-Reswap", "none")
		render(w, r, http.StatusOK, Toast("ok", "Ticket impresso."))
	})
}

// printTicket sends o to the printer, logging a failure here.
func (s *Server) printTicket(o db.Order) error {
	err := s.Print(ticketText(o))
	if err != nil {
		log.Printf("print order %d: %v", o.ID, err)
	}
	return err
}

// ticketText lays the order out in lines of at most ticketWidth characters.
// Each item prints as "1x name" with its price right-aligned on the next line. The kitchen
// gets the same ticket.
func ticketText(o db.Order) string {
	var lines []string
	add := func(l ...string) { lines = append(lines, l...) }
	sep := strings.Repeat("-", ticketWidth)

	add(center("UKEMOCHI"))
	add(leftRight(fmt.Sprintf("Pedido #%d", o.ID), localTime(o.CreatedAt, "02/01 15:04"))...)
	add(wrap(o.ClientLabel())...)
	add(leftRight(channelLabels[o.Channel], strings.ToUpper(consumeLabels[o.Consume]))...)
	if o.Consume == db.ConsumeDelivery && o.Address != "" {
		add(wrap(o.Address)...)
	}
	add(sep)
	for _, it := range o.Items {
		add(wrap(fmt.Sprintf("%dx %s", it.Qty, it.PrintableName))...)
		add(leftRight("", money.Format(it.Qty*it.UnitPriceCents))...)
		if it.Note != "" {
			add(wrap("  obs: " + it.Note)...)
		}
	}
	add(sep)
	add(leftRight("TOTAL", money.Format(o.TotalCents))...)
	var paid int64
	for _, p := range o.Payments {
		paid += p.AmountCents
	}
	switch {
	case len(o.Payments) == 0:
		add("", center("A PAGAR"))
	case len(o.Payments) == 1 && paid == o.TotalCents:
		add(leftRight("PAGO", paymentLabels[o.Payments[0].Method])...)
	default:
		// A split bill lists each part, then what is still to pay.
		for _, p := range o.Payments {
			add(leftRight("PAGO "+paymentLabels[p.Method], money.Format(p.AmountCents))...)
		}
		if paid < o.TotalCents {
			add(leftRight("A PAGAR", money.Format(o.TotalCents-paid))...)
		}
	}
	if o.Notes != "" {
		add(wrap("Obs: " + o.Notes)...)
	}
	return strings.Join(lines, "\n")
}

// ticketHeight is the paper length for text: 2.88mm per line (2.4mm font, 1.2 line height),
// plus 8mm body padding and 2mm slack so rounding never spills onto a second page.
// Worked in tenths of a millimetre to stay in integers.
func ticketHeight(text string) string {
	lines := strings.Count(text, "\n") + 1
	tenths := lines*29 + 100
	return fmt.Sprintf("%d.%dmm", tenths/10, tenths%10)
}

func center(s string) string {
	n := len([]rune(s))
	if n >= ticketWidth {
		return s
	}
	return strings.Repeat(" ", (ticketWidth-n)/2) + s
}

// leftRight puts right at the end of the line. When both don't fit, left is wrapped
// and right goes on a line of its own.
func leftRight(left, right string) []string {
	l, r := len([]rune(left)), len([]rune(right))
	if l+1+r <= ticketWidth {
		return []string{left + strings.Repeat(" ", ticketWidth-l-r) + right}
	}
	return append(wrap(left), strings.Repeat(" ", max(0, ticketWidth-r))+right)
}

// wrap breaks s at spaces into lines of at most ticketWidth runes, splitting longer words.
// Leading spaces of s indent the first line; callers keep them short.
func wrap(s string) []string {
	var lines []string
	cur := []rune(s[:len(s)-len(strings.TrimLeft(s, " "))])
	empty := true // cur holds no word yet
	for _, word := range strings.Fields(s) {
		w := []rune(word)
		if !empty && len(cur)+1+len(w) <= ticketWidth {
			cur = append(append(cur, ' '), w...)
			continue
		}
		if !empty {
			lines, cur = append(lines, string(cur)), nil
		}
		for len(cur)+len(w) > ticketWidth {
			k := ticketWidth - len(cur)
			lines = append(lines, string(cur)+string(w[:k]))
			cur, w = nil, w[k:]
		}
		cur, empty = append(cur, w...), false
	}
	if !empty {
		lines = append(lines, string(cur))
	}
	return lines
}

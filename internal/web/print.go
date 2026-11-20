package web

import (
	"fmt"
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
}

// ticketText lays the order out in lines of at most ticketWidth characters.
func ticketText(o db.Order) string {
	var lines []string
	add := func(l ...string) { lines = append(lines, l...) }
	sep := strings.Repeat("-", ticketWidth)

	add(center("UKEMOCHI"))
	add(leftRight(fmt.Sprintf("Pedido #%d", o.ID), localTime(o.CreatedAt, "02/01 15:04"))...)
	add(wrap(o.ClientName)...)
	add(leftRight(channelLabels[o.Channel], strings.ToUpper(consumeLabels[o.Consume]))...)
	add(sep)
	for _, it := range o.Items {
		add(leftRight(fmt.Sprintf("%dx %s", it.Qty, it.PrintableName), money.Format(it.Qty*it.UnitPriceCents))...)
		if it.Note != "" {
			add(wrap("  obs: " + it.Note)...)
		}
	}
	add(sep)
	add(leftRight("TOTAL", money.Format(o.TotalCents))...)
	if o.Payment != "" {
		add(leftRight("PAGO", paymentLabels[o.Payment])...)
	} else {
		add("A PAGAR")
	}
	if o.Notes != "" {
		add(wrap("Obs: " + o.Notes)...)
	}
	return strings.Join(lines, "\n")
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

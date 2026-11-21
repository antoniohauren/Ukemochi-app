package web

import (
	"fmt"
	"strings"

	"github.com/antoniohauren/ukemochi/internal/db"
	"github.com/antoniohauren/ukemochi/internal/money"
)

// Message types, as used in /pedidos/{id}/mensagem/{tipo}.
const (
	msgConfirm = "confirmacao"
	msgReady   = "pronto"
	msgOut     = "saiu"
)

// orderMessage builds the WhatsApp text of one type for o. See docs/flows.md, Messages.
func orderMessage(o db.Order, kind string) (string, bool) {
	switch kind {
	case msgConfirm:
		var b strings.Builder
		fmt.Fprintf(&b, "Olá, %s! Confirmando seu pedido:\n\n", o.ClientName)
		for _, it := range o.Items {
			fmt.Fprintf(&b, "%dx %s — %s\n", it.Qty, it.NameSnapshot, money.Format(it.Qty*it.UnitPriceCents))
			if it.Note != "" {
				fmt.Fprintf(&b, "   _%s_\n", it.Note)
			}
		}
		fmt.Fprintf(&b, "\n*Total: %s*", money.Format(o.TotalCents))
		return b.String(), true
	case msgReady:
		return o.ClientName + ", seu pedido está pronto! 🍣", true
	case msgOut:
		return o.ClientName + ", seu pedido saiu para entrega! 🛵", true
	}
	return "", false
}

// copyMessages lists the messages worth copying now: the confirmation for WhatsApp orders,
// then "pronto" for eat-in once ready and "saiu" once the courier left.
func copyMessages(o db.Order) []struct{ Kind, Label, Text string } {
	var out []struct{ Kind, Label, Text string }
	add := func(kind, label string) {
		text, _ := orderMessage(o, kind)
		out = append(out, struct{ Kind, Label, Text string }{kind, label, text})
	}
	if o.Channel != "whatsapp" {
		return nil
	}
	if len(o.Items) > 0 {
		add(msgConfirm, "Copiar confirmação")
	}
	if o.Status == db.StatusReady && o.Consume == db.ConsumeLocal {
		add(msgReady, "Copiar \"pronto\"")
	}
	if o.Status == db.StatusOut {
		add(msgOut, "Copiar \"saiu\"")
	}
	return out
}

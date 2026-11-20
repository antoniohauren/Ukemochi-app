package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // the Pi image may ship without zoneinfo

	"github.com/a-h/templ"

	"github.com/antoniohauren/ukemochi/internal/db"
)

// saoPaulo is the shop's time zone for displayed times and day boundaries.
var saoPaulo = func() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return time.FixedZone("BRT", -3*60*60)
	}
	return loc
}()

// inputError is a validation message shown to the user as is.
type inputError string

func (e inputError) Error() string { return string(e) }

const maxFormBytes = 16 << 10

func (s *Server) registerOrders(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.orderList)
	mux.HandleFunc("GET /pedidos/novo", func(w http.ResponseWriter, r *http.Request) {
		render(w, r, http.StatusOK, NewOrderPage(db.NewOrder{Channel: "whatsapp", Consume: db.ConsumeDelivery}, ""))
	})
	mux.HandleFunc("POST /pedidos", s.createOrder)
	mux.HandleFunc("GET /pedidos/{id}", s.orderEditor)
	mux.HandleFunc("POST /pedidos/{id}/itens", s.addItem)
	mux.HandleFunc("PATCH /pedidos/{id}/itens/{item}", s.setItemQty)
	mux.HandleFunc("POST /pedidos/{id}/itens/{item}", s.setItemQty) // form fallback without htmx
	mux.HandleFunc("DELETE /pedidos/{id}/itens/{item}", s.setItemQty)
	mux.HandleFunc("POST /pedidos/{id}/status", s.setStatus)
	mux.HandleFunc("POST /pedidos/{id}/consumo", s.setConsume)
	mux.HandleFunc("GET /pedidos/{id}/mensagem/{tipo}", s.orderMessage)
}

func (s *Server) orderList(w http.ResponseWriter, r *http.Request) {
	orders, err := s.DB.ListOpenOrders(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	render(w, r, http.StatusOK, OrderListPage(orders))
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		fail(w, r, err)
		return
	}
	o, err := parseNewOrder(r)
	if err != nil {
		render(w, r, http.StatusUnprocessableEntity, NewOrderPage(o, err.Error()))
		return
	}
	id, err := s.DB.CreateOrder(r.Context(), o)
	if err != nil {
		fail(w, r, err)
		return
	}
	redirect(w, r, orderURL(id))
}

func parseNewOrder(r *http.Request) (db.NewOrder, error) {
	o := db.NewOrder{
		Channel:    r.PostFormValue("canal"),
		Consume:    r.PostFormValue("consumo"),
		ClientName: strings.TrimSpace(r.PostFormValue("nome")),
		PhoneLast4: strings.TrimSpace(r.PostFormValue("telefone")),
		Notes:      strings.TrimSpace(r.PostFormValue("obs")),
	}
	if o.Consume == "" && o.Channel == "balcao" {
		o.Consume = db.ConsumeLocal
	}
	if o.Consume == "" {
		o.Consume = db.ConsumeDelivery
	}
	switch {
	case o.Channel != "whatsapp" && o.Channel != "balcao":
		return o, inputError("Escolha WhatsApp ou Balcão.")
	case o.Consume != db.ConsumeDelivery && o.Consume != db.ConsumeLocal:
		return o, inputError("Escolha entrega ou consumo no local.")
	case o.ClientName == "":
		return o, inputError("Informe o nome do cliente.")
	case len([]rune(o.ClientName)) > 60:
		return o, inputError("Nome muito longo (máx. 60).")
	case o.PhoneLast4 != "" && !isDigits(o.PhoneLast4, 4):
		return o, inputError("Telefone: informe só os 4 últimos dígitos.")
	case len([]rune(o.Notes)) > 200:
		return o, inputError("Observação muito longa (máx. 200).")
	}
	return o, nil
}

func isDigits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (s *Server) orderEditor(w http.ResponseWriter, r *http.Request) {
	o, ok := s.loadOrder(w, r)
	if !ok {
		return
	}
	var menu []db.Category
	if o.Status == db.StatusOpen {
		var err error
		if menu, err = s.DB.Menu(r.Context(), true); err != nil {
			fail(w, r, err)
			return
		}
	}
	render(w, r, http.StatusOK, OrderEditorPage(o, menu))
}

func (s *Server) addItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, db.ErrNotFound)
		return
	}
	if err := parseForm(w, r); err != nil {
		fail(w, r, err)
		return
	}
	product, err := strconv.ParseInt(r.PostFormValue("produto"), 10, 64)
	if err != nil || product <= 0 {
		fail(w, r, inputError("Produto inválido."))
		return
	}
	if err := s.DB.AddItem(r.Context(), id, product, 1, ""); err != nil {
		fail(w, r, err)
		return
	}
	s.itemsChanged(w, r, id, "Item adicionado")
}

// setItemQty handles PATCH (and its POST fallback) with qty, and DELETE. A qty of 0 removes the line.
func (s *Server) setItemQty(w http.ResponseWriter, r *http.Request) {
	id, ok1 := pathID(r, "id")
	item, ok2 := pathID(r, "item")
	if !ok1 || !ok2 {
		fail(w, r, db.ErrNotFound)
		return
	}
	var qty int64
	if r.Method != http.MethodDelete {
		if err := parseForm(w, r); err != nil {
			fail(w, r, err)
			return
		}
		var err error
		qty, err = strconv.ParseInt(r.PostFormValue("qty"), 10, 64)
		if err != nil || qty < 0 || qty > db.MaxQty {
			fail(w, r, inputError(fmt.Sprintf("Quantidade deve ser de 0 a %d.", db.MaxQty)))
			return
		}
	}
	if err := s.DB.SetItemQty(r.Context(), id, item, qty); err != nil {
		fail(w, r, err)
		return
	}
	s.itemsChanged(w, r, id, "")
}

// itemsChanged answers an item edit: the new panel for htmx, a redirect to the editor otherwise.
func (s *Server) itemsChanged(w http.ResponseWriter, r *http.Request, id int64, toast string) {
	if !isHX(r) {
		redirect(w, r, orderURL(id))
		return
	}
	o, err := s.DB.GetOrder(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	render(w, r, http.StatusOK, ItemsUpdate(o, toast))
}

func (s *Server) setStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, db.ErrNotFound)
		return
	}
	if err := parseForm(w, r); err != nil {
		fail(w, r, err)
		return
	}
	to := r.PostFormValue("para")
	if err := s.DB.SetStatus(r.Context(), id, to); err != nil {
		fail(w, r, err)
		return
	}
	if to == db.StatusKitchen {
		redirect(w, r, orderURL(id)+"/imprimir")
		return
	}
	redirect(w, r, orderURL(id))
}

func (s *Server) setConsume(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, db.ErrNotFound)
		return
	}
	if err := parseForm(w, r); err != nil {
		fail(w, r, err)
		return
	}
	if err := s.DB.SetConsume(r.Context(), id, r.PostFormValue("consumo")); err != nil {
		fail(w, r, err)
		return
	}
	redirect(w, r, orderURL(id))
}

func (s *Server) orderMessage(w http.ResponseWriter, r *http.Request) {
	o, ok := s.loadOrder(w, r)
	if !ok {
		return
	}
	msg, ok := orderMessage(o, r.PathValue("tipo"))
	if !ok {
		fail(w, r, db.ErrNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, msg)
}

// loadOrder reads the {id} path value and its order, answering the request itself on failure.
func (s *Server) loadOrder(w http.ResponseWriter, r *http.Request) (db.Order, bool) {
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, db.ErrNotFound)
		return db.Order{}, false
	}
	o, err := s.DB.GetOrder(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return o, false
	}
	return o, true
}

func orderURL(id int64) string { return "/pedidos/" + strconv.FormatInt(id, 10) }

func isHX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func parseForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		return inputError("Formulário inválido.")
	}
	return nil
}

func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		log.Printf("render %s %s: %v", r.Method, r.URL.Path, err)
	}
}

// redirect sends the browser to url after a write: HX-Redirect for htmx, 303 otherwise.
func redirect(w http.ResponseWriter, r *http.Request, url string) {
	if isHX(r) {
		w.Header().Set("HX-Redirect", url)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// fail shows err to the user: a toast for htmx requests, an error page otherwise.
// Unexpected errors are logged here, once.
func fail(w http.ResponseWriter, r *http.Request, err error) {
	status, msg := errorStatus(err)
	if status == http.StatusInternalServerError {
		log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
	}
	if isHX(r) {
		// htmx skips swaps, even out-of-band ones, on 4xx/5xx, so the toast goes out as 200.
		w.Header().Set("HX-Reswap", "none")
		c := Toast("error", msg)
		if r.Header.Get("HX-Target") == "pedido-itens" {
			// The toast sits under the open order sheet (a modal dialog), so repeat it inside the sheet.
			c = templ.Join(c, sheetError(msg))
		}
		render(w, r, http.StatusOK, c)
		return
	}
	render(w, r, status, ErrorPage(msg))
}

func errorStatus(err error) (int, string) {
	var in inputError
	switch {
	case errors.As(err, &in):
		return http.StatusUnprocessableEntity, string(in)
	case errors.Is(err, db.ErrNotFound):
		return http.StatusNotFound, "Não encontrado."
	case errors.Is(err, db.ErrUnpaid):
		return http.StatusConflict, "Registre o pagamento antes de fechar o pedido."
	case errors.Is(err, db.ErrNotEditable):
		return http.StatusConflict, "Itens só podem ser alterados com o pedido aberto."
	case errors.Is(err, db.ErrBadTransition):
		return http.StatusConflict, "Mudança de status não permitida."
	case errors.Is(err, db.ErrEmptyOrder):
		return http.StatusUnprocessableEntity, "Adicione itens antes de enviar à cozinha."
	case errors.Is(err, db.ErrInvalid):
		return http.StatusUnprocessableEntity, "Valor inválido."
	}
	return http.StatusInternalServerError, "Erro interno. Tente de novo."
}

func localTime(ts, layout string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	return t.In(saoPaulo).Format(layout)
}

var channelLabels = map[string]string{"whatsapp": "WhatsApp", "balcao": "Balcão"}
var consumeLabels = map[string]string{db.ConsumeDelivery: "Entrega", db.ConsumeLocal: "No local"}

// statusActions lists the status buttons in display order; only allowed moves are shown.
var statusActions = []struct{ To, Label, Variant string }{
	{db.StatusKitchen, "Enviar à cozinha", "primary"},
	{db.StatusReady, "Marcar pronto", "primary"},
	{db.StatusOut, "Saiu p/ entrega", "primary"},
	{db.StatusClosed, "Fechar", "primary"},
	{db.StatusOpen, "Reabrir", "secondary"},
	{db.StatusCancelled, "Cancelar pedido", "danger"},
}

func itemCount(o db.Order) int64 {
	var n int64
	for _, it := range o.Items {
		n += it.Qty
	}
	return n
}

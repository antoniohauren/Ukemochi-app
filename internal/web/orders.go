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
		render(w, r, http.StatusOK, NewOrderPage(db.NewOrder{Channel: "whatsapp", Consume: db.ConsumeDelivery}, "", ""))
	})
	mux.HandleFunc("POST /pedidos", s.createOrder)
	mux.HandleFunc("GET /pedidos/{id}", s.orderEditor)
	mux.HandleFunc("GET /pedidos/{id}/adicionar", s.addItemStep)
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
	o, field, err := parseNewOrder(r)
	if err != nil {
		render(w, r, http.StatusUnprocessableEntity, NewOrderPage(o, err.Error(), field))
		return
	}
	id, err := s.DB.CreateOrder(r.Context(), o)
	if err != nil {
		fail(w, r, err)
		return
	}
	redirect(w, r, orderURL(id))
}

// parseNewOrder reads the new-order form. On error, field names the input at fault ("" for the radio choices).
func parseNewOrder(r *http.Request) (o db.NewOrder, field string, err error) {
	o = db.NewOrder{
		Channel:    r.PostFormValue("canal"),
		Consume:    r.PostFormValue("consumo"),
		ClientName: strings.TrimSpace(r.PostFormValue("nome")),
		PhoneLast4: strings.TrimSpace(r.PostFormValue("telefone")),
		Notes:      strings.TrimSpace(r.PostFormValue("obs")),
		Address:    strings.TrimSpace(r.PostFormValue("endereco")),
	}
	// Balcão is in-house, so it is always eaten in. The form's consumo radio always posts a value.
	if o.Channel == "balcao" {
		o.Consume = db.ConsumeLocal
	}
	if o.Consume == "" {
		o.Consume = db.ConsumeDelivery
	}
	if o.Consume == db.ConsumeLocal {
		o.Address = ""
	}
	switch {
	case o.Channel != "whatsapp" && o.Channel != "balcao":
		return o, "", inputError("Escolha WhatsApp ou Balcão.")
	case o.Consume != db.ConsumeDelivery && o.Consume != db.ConsumeLocal:
		return o, "", inputError("Escolha entrega ou consumo no local.")
	}
	if err := checkAddress(o.Consume, o.Address); err != nil {
		return o, "endereco", err
	}
	if field, err := checkCustomer(o.ClientName, o.PhoneLast4, o.Notes); err != nil {
		return o, field, err
	}
	return o, "", nil
}

// checkAddress requires a delivery address for entrega.
func checkAddress(consume, address string) error {
	switch {
	case consume == db.ConsumeDelivery && address == "":
		return inputError("Informe o endereço de entrega.")
	case len([]rune(address)) > maxAddress:
		return inputError("Endereço muito longo (máx. 120).")
	}
	return nil
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
	if canEditItems(o) {
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
	qty := int64(1) // a post without qty adds one
	if v := r.PostFormValue("qty"); v != "" {
		if qty, err = strconv.ParseInt(v, 10, 64); err != nil || qty < 1 || qty > db.MaxQty {
			fail(w, r, inputError(fmt.Sprintf("Quantidade deve ser de 1 a %d.", db.MaxQty)))
			return
		}
	}
	if err := s.DB.AddItem(r.Context(), id, product, qty, ""); err != nil {
		fail(w, r, err)
		return
	}
	if !isHX(r) {
		redirect(w, r, orderURL(id))
		return
	}
	o, err := s.DB.GetOrder(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	menu, err := s.DB.Menu(r.Context(), true)
	if err != nil {
		fail(w, r, err)
		return
	}
	// The add sheet stays open on the product's category, so the next item from it is one tap away.
	render(w, r, http.StatusOK, ItemAdded(o, menu, product))
}

// addItemStep shows one step of adding an item: categories, then the category's
// items (?categoria=), the item's products (?item=), then the quantity for one product (?produto=).
func (s *Server) addItemStep(w http.ResponseWriter, r *http.Request) {
	o, ok := s.loadOrder(w, r)
	if !ok {
		return
	}
	if !canEditItems(o) {
		redirect(w, r, orderURL(o.ID))
		return
	}
	menu, err := s.DB.Menu(r.Context(), true)
	if err != nil {
		fail(w, r, err)
		return
	}
	step, title, err := pickStep(r, o, menu)
	if err != nil {
		fail(w, r, err)
		return
	}
	if isHX(r) {
		step = AddStepHX(title, step)
	} else {
		step = AddItemPage(o, title, step)
	}
	render(w, r, http.StatusOK, step)
}

// pickStep returns the add-item step for the query and the title naming the current pick.
func pickStep(r *http.Request, o db.Order, menu []db.Category) (templ.Component, string, error) {
	orderID := o.ID
	q := r.URL.Query()
	if v := q.Get("produto"); v != "" {
		for _, c := range menu {
			for _, it := range c.Items {
				for _, p := range it.Products {
					if strconv.FormatInt(p.ID, 10) == v {
						return AddQtyStep(orderID, c, it, p, reopenNote(o)), db.DisplayName(it.Name, p.Variant, p.Size), nil
					}
				}
			}
		}
		return nil, "", db.ErrNotFound
	}
	if v := q.Get("item"); v != "" {
		for _, c := range menu {
			for _, it := range c.Items {
				if strconv.FormatInt(it.ID, 10) == v {
					return AddVariantStep(orderID, c, it), it.Name, nil
				}
			}
		}
		return nil, "", db.ErrNotFound
	}
	if v := q.Get("categoria"); v != "" {
		for _, c := range menu {
			if strconv.FormatInt(c.ID, 10) == v {
				return AddItemStep(orderID, c), c.Name, nil
			}
		}
		return nil, "", db.ErrNotFound
	}
	return AddCategoryStep(orderID, menu), "Adicionar item", nil
}

// setItemQty handles PATCH (and its POST fallback) with qty and/or nota, and DELETE. A qty of 0 removes the line.
// A nota without qty changes only the line's note.
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
		if _, ok := r.PostForm["nota"]; ok {
			note := strings.TrimSpace(r.PostFormValue("nota"))
			if len([]rune(note)) > db.MaxItemNote {
				fail(w, r, inputError(fmt.Sprintf("Observação do item muito longa (máx. %d).", db.MaxItemNote)))
				return
			}
			if err := s.DB.SetItemNote(r.Context(), id, item, note); err != nil {
				fail(w, r, err)
				return
			}
			if r.PostFormValue("qty") == "" {
				s.itemsChanged(w, r, id, "Observação salva")
				return
			}
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
		// The status already changed, so a print failure falls back to the browser ticket page.
		if s.Print == nil {
			redirect(w, r, orderURL(id)+"/imprimir")
			return
		}
		o, err := s.DB.GetOrder(r.Context(), id)
		if err != nil || s.printTicket(o) != nil {
			redirect(w, r, orderURL(id)+"/imprimir")
			return
		}
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
	consume, address := r.PostFormValue("consumo"), strings.TrimSpace(r.PostFormValue("endereco"))
	if consume != db.ConsumeDelivery {
		address = ""
	}
	if err := checkAddress(consume, address); err != nil {
		fail(w, r, err)
		return
	}
	if err := s.DB.SetConsume(r.Context(), id, consume, address); err != nil {
		fail(w, r, err)
		return
	}
	if !isHX(r) {
		http.Redirect(w, r, orderURL(id), http.StatusSeeOther)
		return
	}
	o, err := s.DB.GetOrder(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	// Fired on the form before the swap, so the sheet can close and the actions sheet reopen.
	w.Header().Set("HX-Trigger", "consumo-salvo")
	render(w, r, http.StatusOK, ConsumeUpdate(o))
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
		// The toast sits under an open sheet (a modal dialog), so repeat it inside the sheet.
		switch r.Header.Get("HX-Target") {
		case "pedido-itens":
			c = templ.Join(c, sheetError("sheet-error", msg))
		case "pagamento":
			c = templ.Join(c, sheetError("pagamento-erro", msg))
		case "acoes":
			c = templ.Join(c, sheetError("acoes-erro", msg))
		case "consumo":
			c = templ.Join(c, sheetError("consumo-erro", msg))
		case "cliente":
			c = templ.Join(c, sheetError("cliente-erro", msg))
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
	case errors.Is(err, db.ErrPaidMismatch):
		return http.StatusConflict, "Os pagamentos não batem com o total. Atualize o pagamento."
	case errors.Is(err, db.ErrOverpaid):
		return http.StatusUnprocessableEntity, "Valor maior que o que falta pagar."
	case errors.Is(err, db.ErrCashShort):
		return http.StatusUnprocessableEntity, "Valor recebido menor que o valor a pagar."
	case errors.Is(err, db.ErrNotEditable):
		return http.StatusConflict, "Itens não podem ser alterados depois que o pedido saiu ou foi fechado."
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

const maxAddress = 120

var consumeLabels = map[string]string{db.ConsumeDelivery: "Entrega", db.ConsumeLocal: "No local"}

// canChangeConsume reports whether o may still switch between entrega and local (db.SetConsume).
func canChangeConsume(o db.Order) bool {
	return o.Status == db.StatusOpen || o.Status == db.StatusKitchen || o.Status == db.StatusReady
}

// statusActions lists the status buttons in display order; only allowed moves are shown.
var statusActions = []struct{ To, Label, Variant string }{
	{db.StatusKitchen, "Enviar à cozinha", "primary"},
	{db.StatusReady, "Marcar pronto", "primary"},
	{db.StatusOut, "Saiu p/ entrega", "primary"},
	{db.StatusClosed, "Fechar", "primary"},
	{db.StatusOpen, "Reabrir", "secondary"},
	{db.StatusCancelled, "Cancelar pedido", "danger"},
}

// reopenNote is what an item change says before it sends a cozinha or pronto order back to aberto; "" otherwise.
func reopenNote(o db.Order) string {
	if o.Status == db.StatusKitchen || o.Status == db.StatusReady {
		return "O pedido volta para Aberto e precisa ser enviado à cozinha de novo."
	}
	return ""
}

// productCategory finds the category holding product.
func productCategory(menu []db.Category, product int64) (db.Category, bool) {
	for _, c := range menu {
		for _, it := range c.Items {
			for _, p := range it.Products {
				if p.ID == product {
					return c, true
				}
			}
		}
	}
	return db.Category{}, false
}

// canEditItems reports whether o's items may change (db.AddItem); a change in cozinha or pronto moves it to aberto.
func canEditItems(o db.Order) bool {
	return o.Status == db.StatusOpen || o.Status == db.StatusKitchen || o.Status == db.StatusReady
}

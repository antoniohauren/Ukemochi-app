package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/antoniohauren/ukemochi/internal/db"
	"github.com/antoniohauren/ukemochi/internal/money"
)

const menuURL = "/admin/cardapio"

func (s *Server) registerMenu(mux *http.ServeMux) {
	admin := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.requireAdmin(h)) }
	admin("GET "+menuURL, func(w http.ResponseWriter, r *http.Request) { s.renderMenu(w, r, http.StatusOK, "") })
	admin("POST "+menuURL+"/categorias", s.menuWrite(s.saveCategory))
	admin("POST "+menuURL+"/categorias/{id}", s.menuWrite(s.saveCategory))
	admin("POST "+menuURL+"/categorias/{id}/mover", s.menuWrite(s.moveCategory))
	admin("POST "+menuURL+"/itens", s.menuWrite(s.saveItem))
	admin("POST "+menuURL+"/itens/{id}", s.menuWrite(s.saveItem))
	admin("POST "+menuURL+"/produtos", s.menuWrite(s.saveProduct))
	admin("POST "+menuURL+"/produtos/{id}", s.menuWrite(s.saveProduct))
}

// menuWrite parses the form, runs save and goes back to the menu page at the anchor save returns.
// A path without {id} creates a row; with {id} it updates that row.
func (s *Server) menuWrite(save func(ctx context.Context, r *http.Request, id int64, menu []db.Category) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id int64
		if r.PathValue("id") != "" {
			var ok bool
			if id, ok = pathID(r, "id"); !ok {
				fail(w, r, db.ErrNotFound)
				return
			}
		}
		if err := parseForm(w, r); err != nil {
			fail(w, r, err)
			return
		}
		menu, err := s.DB.Menu(r.Context(), false)
		if err != nil {
			fail(w, r, err)
			return
		}
		anchor, err := save(r.Context(), r, id, menu)
		if err != nil {
			if status, msg := errorStatus(err); status != http.StatusInternalServerError {
				s.renderMenu(w, r, status, msg)
				return
			}
			fail(w, r, err)
			return
		}
		http.Redirect(w, r, menuURL+"#"+anchor, http.StatusSeeOther)
	}
}

func (s *Server) renderMenu(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	menu, err := s.DB.Menu(r.Context(), false)
	if err != nil {
		fail(w, r, err)
		return
	}
	render(w, r, status, MenuPage(menu, errMsg))
}

func (s *Server) saveCategory(ctx context.Context, r *http.Request, id int64, menu []db.Category) (string, error) {
	name, err := formText(r, "nome", "Nome", 40, true)
	if err != nil {
		return "", err
	}
	if id == 0 {
		// New categories go last; menu is sorted by position.
		var pos int64
		if len(menu) > 0 {
			pos = menu[len(menu)-1].Position + 1
		}
		id, err = s.DB.CreateCategory(ctx, name, pos)
	} else {
		err = s.DB.UpdateCategory(ctx, id, name)
	}
	if errors.Is(err, db.ErrDuplicate) {
		return "", inputError("Já existe uma categoria com esse nome.")
	}
	return "cat-" + strconv.FormatInt(id, 10), err
}

// moveCategory moves a category one step; direcao is "cima" or "baixo".
func (s *Server) moveCategory(ctx context.Context, r *http.Request, id int64, _ []db.Category) (string, error) {
	dir := r.PostFormValue("direcao")
	if dir != "cima" && dir != "baixo" {
		return "", inputError("Direção inválida.")
	}
	return "cat-" + strconv.FormatInt(id, 10), s.DB.MoveCategory(ctx, id, dir == "cima")
}

func (s *Server) saveItem(ctx context.Context, r *http.Request, id int64, menu []db.Category) (string, error) {
	it := db.Item{ID: id, Active: id == 0 || r.PostFormValue("ativo") != ""}
	var err error
	if it.CategoryID, err = formInt(r, "categoria", "Categoria"); err != nil {
		return "", err
	}
	if !hasCategory(menu, it.CategoryID) {
		return "", inputError("Categoria inválida.")
	}
	if it.Name, err = formText(r, "nome", "Nome", 60, true); err != nil {
		return "", err
	}
	if it.Description, err = formText(r, "descricao", "Descrição", 120, false); err != nil {
		return "", err
	}
	if id == 0 {
		id, err = s.DB.CreateItem(ctx, it)
	} else {
		err = s.DB.UpdateItem(ctx, it)
	}
	return "item-" + strconv.FormatInt(id, 10), err
}

func (s *Server) saveProduct(ctx context.Context, r *http.Request, id int64, menu []db.Category) (string, error) {
	p := db.Product{ID: id, Active: id == 0 || r.PostFormValue("ativo") != ""}
	var err error
	if id == 0 {
		if p.ItemID, err = formInt(r, "item", "Item"); err != nil {
			return "", err
		}
		if !hasItem(menu, p.ItemID) {
			return "", inputError("Item inválido.")
		}
	}
	if p.Variant, err = formText(r, "variante", "Variante", 40, false); err != nil {
		return "", err
	}
	if p.Size, err = formText(r, "tamanho", "Tamanho", 40, false); err != nil {
		return "", err
	}
	if p.PrintableName, err = formText(r, "impresso", "Nome impresso", ticketWidth, true); err != nil {
		return "", err
	}
	if p.PriceCents, err = money.Parse(r.PostFormValue("preco")); err != nil {
		return "", inputError("Preço inválido. Ex.: 12,50")
	}
	if id == 0 {
		_, err = s.DB.CreateProduct(ctx, p)
		return "item-" + strconv.FormatInt(p.ItemID, 10), err
	}
	if err = s.DB.UpdateProduct(ctx, p); err != nil {
		return "", err
	}
	return "item-" + strconv.FormatInt(productItem(menu, id), 10), nil
}

// formText returns the trimmed field, checking that it is present when required and at most maxLen runes.
func formText(r *http.Request, key, label string, maxLen int, required bool) (string, error) {
	v := strings.TrimSpace(r.PostFormValue(key))
	if required && v == "" {
		return "", inputError("Informe: " + label + ".")
	}
	if len([]rune(v)) > maxLen {
		return "", inputError(label + ": máximo de " + strconv.Itoa(maxLen) + " caracteres.")
	}
	return v, nil
}

func formInt(r *http.Request, key, label string) (int64, error) {
	v := strings.TrimSpace(r.PostFormValue(key))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 || n > 1_000_000 {
		return 0, inputError(label + " inválido(a).")
	}
	return n, nil
}

func hasCategory(menu []db.Category, id int64) bool {
	for _, c := range menu {
		if c.ID == id {
			return true
		}
	}
	return false
}

func hasItem(menu []db.Category, id int64) bool {
	for _, c := range menu {
		for _, it := range c.Items {
			if it.ID == id {
				return true
			}
		}
	}
	return false
}

// productItem returns the item id of product id, or 0.
func productItem(menu []db.Category, id int64) int64 {
	for _, c := range menu {
		for _, it := range c.Items {
			for _, p := range it.Products {
				if p.ID == id {
					return it.ID
				}
			}
		}
	}
	return 0
}

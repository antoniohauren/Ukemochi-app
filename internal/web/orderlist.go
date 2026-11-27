package web

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/antoniohauren/ukemochi/internal/db"
)

// orderFilter narrows the open-order list by status and consume type. An empty field means all.
type orderFilter struct {
	Status  string
	Consume string
}

// listStatuses are the status tabs in flow order. fechado and cancelado never reach the list.
var listStatuses = []string{db.StatusOpen, db.StatusKitchen, db.StatusReady, db.StatusOut}

var listStatusLabels = map[string]string{
	db.StatusOpen:    "Abertos",
	db.StatusKitchen: "Cozinha",
	db.StatusReady:   "Prontos",
	db.StatusOut:     "Saiu",
}

var listConsumes = []string{db.ConsumeDelivery, db.ConsumeLocal, db.ConsumePickup}

// filterCookie remembers the last filter, so coming back from an order keeps the same tab.
const filterCookie = "pedidos_filtro"

func validListStatus(s string) bool {
	return s == "" || listStatusLabels[s] != ""
}

func validListConsume(c string) bool {
	return c == "" || db.ValidConsume(c)
}

// URL is the list address for f. Both keys are always present, so "all" differs from "no filter given".
func (f orderFilter) URL() string {
	return "/?" + url.Values{"status": {f.Status}, "consumo": {f.Consume}}.Encode()
}

func (f orderFilter) match(o db.Order) bool {
	return (f.Status == "" || o.Status == f.Status) && (f.Consume == "" || o.Consume == f.Consume)
}

// orderTab is one filter chip: its label, where it leads, how many orders it holds and whether it is on.
type orderTab struct {
	Label  string
	URL    string
	Count  int
	Active bool
}

// statusTabs counts each status within the current consume filter, so the numbers match what a tap shows.
func statusTabs(orders []db.Order, f orderFilter) []orderTab {
	tab := func(label, status string) orderTab {
		g := orderFilter{Status: status, Consume: f.Consume}
		return orderTab{Label: label, URL: g.URL(), Count: countOrders(orders, g), Active: f.Status == status}
	}
	tabs := []orderTab{tab("Todos", "")}
	for _, st := range listStatuses {
		tabs = append(tabs, tab(listStatusLabels[st], st))
	}
	return tabs
}

// consumeTabs counts each consume type within the current status filter.
func consumeTabs(orders []db.Order, f orderFilter) []orderTab {
	tab := func(label, consume string) orderTab {
		g := orderFilter{Status: f.Status, Consume: consume}
		return orderTab{Label: label, URL: g.URL(), Count: countOrders(orders, g), Active: f.Consume == consume}
	}
	tabs := []orderTab{tab("Todos", "")}
	for _, c := range listConsumes {
		tabs = append(tabs, tab(consumeLabels[c], c))
	}
	return tabs
}

func countOrders(orders []db.Order, f orderFilter) int {
	n := 0
	for _, o := range orders {
		if f.match(o) {
			n++
		}
	}
	return n
}

// readFilter takes the filter from the query, or from the cookie when the query names none (a plain "/").
func readFilter(r *http.Request) (orderFilter, error) {
	q := r.URL.Query()
	if !q.Has("status") && !q.Has("consumo") {
		if c, err := r.Cookie(filterCookie); err == nil {
			st, co, _ := strings.Cut(c.Value, ".")
			if validListStatus(st) && validListConsume(co) {
				return orderFilter{Status: st, Consume: co}, nil
			}
		}
		return orderFilter{}, nil
	}
	f := orderFilter{Status: q.Get("status"), Consume: q.Get("consumo")}
	if !validListStatus(f.Status) || !validListConsume(f.Consume) {
		return orderFilter{}, inputError("Filtro inválido.")
	}
	return f, nil
}

func (s *Server) orderList(w http.ResponseWriter, r *http.Request) {
	f, err := readFilter(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	orders, err := s.DB.ListOpenOrders(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	// The value is two known words, so it needs no encoding. It expires overnight, so the next day starts on Todos.
	http.SetCookie(w, &http.Cookie{
		Name: filterCookie, Value: f.Status + "." + f.Consume, Path: "/",
		MaxAge: int((12 * time.Hour).Seconds()), HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	shown := make([]db.Order, 0, len(orders))
	for _, o := range orders {
		if f.match(o) {
			shown = append(shown, o)
		}
	}
	render(w, r, http.StatusOK, OrderListPage(shown, len(orders), f, statusTabs(orders, f), consumeTabs(orders, f)))
}

// activeTab is the chosen tab, or the first (Todos) if none is.
func activeTab(tabs []orderTab) orderTab {
	for _, t := range tabs {
		if t.Active {
			return t
		}
	}
	return tabs[0]
}

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/antoniohauren/ukemochi/internal/db"
)

func TestOrderListFilters(t *testing.T) {
	s, h, prod := dbServer(t)
	ctx := context.Background()
	add := func(name, consume, status string) {
		t.Helper()
		o := db.NewOrder{Channel: "whatsapp", Consume: consume, ClientName: name}
		if consume == db.ConsumeDelivery {
			o.Address = "Rua A, 1"
		}
		id, err := s.DB.CreateOrder(ctx, o)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.DB.AddItem(ctx, id, prod, 1, ""); err != nil {
			t.Fatal(err)
		}
		if status != db.StatusOpen {
			if err := s.DB.SetStatus(ctx, id, status); err != nil {
				t.Fatal(err)
			}
		}
	}
	add("Ana", db.ConsumeDelivery, db.StatusOpen)
	add("Bia", db.ConsumeDelivery, db.StatusReady)
	add("Caio", db.ConsumeLocal, db.StatusReady)
	add("Duda", db.ConsumeLocal, db.StatusOpen)
	if err := s.DB.SetStatus(ctx, 4, db.StatusCancelled); err != nil {
		t.Fatal(err)
	}

	shows := func(b string, names ...string) {
		t.Helper()
		for _, n := range []string{"Ana", "Bia", "Caio", "Duda"} {
			want := false
			for _, m := range names {
				want = want || m == n
			}
			if got := strings.Contains(b, " "+n+"</p>"); got != want {
				t.Errorf("%s shown = %v, want %v", n, got, want)
			}
		}
	}
	shows(do(h, "GET", "/", nil, false).Body.String(), "Ana", "Bia", "Caio")
	// Left to deliver: entrega and ready.
	b := do(h, "GET", "/?status=pronto&consumo=entrega", nil, false).Body.String()
	shows(b, "Bia")
	// Status counts follow the consume filter: 1 open delivery, 1 ready delivery. The poll keeps the filter.
	if !regexp.MustCompile(`(?s)>Abertos</span>\s*<span[^>]*>1<`).MatchString(b) || !regexp.MustCompile(`(?s)aria-current="true"[^>]*>\s*<span[^>]*>Prontos<`).MatchString(b) {
		t.Errorf("tabs: %s", b)
	}
	// Each button names its current choice and opens its sheet.
	for _, want := range []string{`getElementById(&#39;filtro-status&#39;).showModal()`, `id="filtro-status"`, `id="filtro-tipo"`} {
		if !strings.Contains(b, want) {
			t.Errorf("missing %s", want)
		}
	}
	if !regexp.MustCompile(`(?s)aria-haspopup="dialog".*?>Prontos <span[^>]*>\(1\)`).MatchString(b) {
		t.Error("status button should show Prontos (1)")
	}
	if !strings.Contains(b, `hx-get="/?consumo=entrega&amp;status=pronto" hx-trigger="every 15s`) {
		t.Error("poll should keep the filter")
	}
	shows(do(h, "GET", "/?status=&consumo=local", nil, false).Body.String(), "Caio")
	if b := do(h, "GET", "/?status=saiu&consumo=", nil, false).Body.String(); !strings.Contains(b, "Nenhum pedido neste filtro.") {
		t.Error("empty filter needs its own message")
	}

	for _, bad := range []string{"/?status=fechado", "/?consumo=x", "/?status=cozinha%27"} {
		if rec := do(h, "GET", bad, nil, false); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: code %d", bad, rec.Code)
		}
	}
}

func TestOrderListRemembersFilter(t *testing.T) {
	_, h, _ := dbServer(t)
	rec := do(h, "GET", "/?status=pronto&consumo=entrega", nil, false)
	var c *http.Cookie
	for _, k := range rec.Result().Cookies() {
		if k.Name == filterCookie {
			c = k
		}
	}
	if c == nil || c.Value != "pronto.entrega" {
		t.Fatalf("cookie: %+v", c)
	}
	get := func(target string, c *http.Cookie) string {
		req := httptest.NewRequest("GET", target, nil)
		req.AddCookie(c)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Body.String()
	}
	active := regexp.MustCompile(`(?s)aria-current="true"[^>]*>\s*<span[^>]*>(\w+)<`)
	tabs := func(b string) []string {
		var out []string
		for _, m := range active.FindAllStringSubmatch(b, -1) {
			out = append(out, m[1])
		}
		return out
	}
	// A plain "/" (the home link) brings the last filter back.
	if got := tabs(get("/", c)); strings.Join(got, ",") != "Prontos,Entrega" {
		t.Errorf("remembered tabs = %v", got)
	}
	// Todos sends both keys empty, which wins over the cookie.
	if got := tabs(get("/?status=&consumo=", c)); strings.Join(got, ",") != "Todos,Todos" {
		t.Errorf("explicit all = %v", got)
	}
	// A tampered cookie is ignored.
	if got := tabs(get("/", &http.Cookie{Name: filterCookie, Value: "x.y"})); strings.Join(got, ",") != "Todos,Todos" {
		t.Errorf("bad cookie = %v", got)
	}
}

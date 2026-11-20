// Package seed fills an empty menu with the restaurant's menu.
// It refuses to touch a menu that already has categories.
package seed

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/antoniohauren/ukemochi/internal/db"
)

type product struct {
	variant, size, printable string
	cents                    int64
}

type item struct {
	name, description string
	products          []product
}

// row is one sized item: its name, its price in reais for each size, and an optional short ticket label.
type row struct {
	name, label string
	reais       []int64
}

func r(name string, reais ...int64) row { return row{name: name, reais: reais} }

func (x row) short(label string) row { x.label = label; return x }

var countTag = regexp.MustCompile(`\(.+\)`)

// sized makes one item per row and one product per size, labelled "<prefix> <short> <(count) or size>".
func sized(prefix string, sizes []string, rows ...row) []item {
	items := make([]item, 0, len(rows))
	for _, x := range rows {
		short := x.label
		if short == "" {
			short = titleCase(x.name)
		}
		it := item{name: x.name}
		for i, size := range sizes {
			// The ticket puts a half first and keeps only the count: "1/2 (4un)" prints as "1/2 Ura Salmão (4un)".
			half, rest := "", size
			if r, ok := strings.CutPrefix(size, "1/2"); ok {
				half, rest = "1/2", r
			}
			tag := rest
			if m := countTag.FindString(rest); m != "" {
				tag = m
			}
			label := strings.Join(strings.Fields(half+" "+prefix+" "+short+" "+tag), " ")
			it.products = append(it.products, product{size: size, printable: label, cents: x.reais[i] * 100})
		}
		items = append(items, it)
	}
	return items
}

func single(name string, reais int64, label, description string) item {
	return item{name, description, []product{{printable: label, cents: reais * 100}}}
}

// titleCase uppercases the first letter of each word, like PHP's ucwords.
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		r, n := utf8.DecodeRuneInString(w)
		words[i] = string(unicode.ToUpper(r)) + w[n:]
	}
	return strings.Join(words, " ")
}

var (
	meio4 = []string{"1/2 (4un)", "(8un)"}
	meio5 = []string{"1/2 (5un)", "(10un)"}
)

// sample is the Nordestino Ksa do Sushi menu. Ticket labels stay within 30 runes so lines never wrap.
var sample = []struct {
	category string
	items    []item
}{
	{"Uramaki", sized("Ura", meio4,
		r("Salmão", 13, 26),
		r("Salmão cheese", 14, 27),
		r("Salmão grelhado", 13, 26),
		r("Camarão cheese", 13, 25),
		r("Atum grelhado", 12, 23),
		r("Skin cheese", 12, 23),
		r("Kani cheese", 13, 25),
	)},
	{"Futomaki", sized("Futo", meio5,
		r("Salmão", 14, 27),
		r("Salmão cheese", 14, 28),
		r("Salmão grelhado", 14, 27),
		r("Camarão cheese", 13, 26),
		r("Atum manga e pepino", 13, 25).short("Atum Mng Pepino"),
		r("Skin cheese", 12, 24),
		r("Kani salmão cheese", 14, 27).short("Kani Salm Chs"),
		r("Califórnia", 13, 25),
	)},
	{"Hots", sized("Hot", meio5,
		r("Hot filadélfia", 16, 32).short("Filadélfia"),
		r("Dinamite", 16, 32),
		r("Camarão cheese", 16, 32),
		r("Hot atum grelhado", 16, 32).short("Atum Grelhado"),
	)},
	{"Kani", sized("Kani", meio4,
		r("Kani cheese", 14, 28).short("Cheese"),
		r("Kani shake", 15, 30).short("Shake"),
		r("Kani atum", 13, 25).short("Atum"),
	)},
	{"Sashimi", sized("Sashimi", []string{"(1un)"},
		r("Salmão", 6),
	)},
	{"Onigiri", sized("Onigiri", []string{"(2un)"},
		r("Salmão grelhado", 26),
		r("Camarão cheese", 24),
		r("Atum grelhado", 23),
		r("Kani skin cheese", 22),
	)},
	{"Especiais", sized("", meio4,
		r("Ura camarão empanado", 14, 28).short("Ura Camarão Empanado"),
		r("Ura dragon camarão", 18, 36).short("Dragon Camarão"),
		r("Ura dragon skin", 16, 32).short("Dragon Skin"),
		r("Jyo", 17, 34),
		r("Bora bora", 18, 36),
		r("Salmão maçaricado", 17, 34).short("Salmão Maçaricado"),
		r("Salmão ebi empanado", 18, 36).short("Salmão Ebi Empanado"),
	)},
	{"Guioza", sized("Guioza", []string{"(6un)"},
		r("Bovina", 18),
		r("Suína", 18),
	)},
	{"Temaki", append(sized("Temaki", []string{""},
		r("Salmão cheese", 32),
		r("Salmão maçaricado", 32).short("Salmão Maçaricado"),
		r("Salmão cebola roxa", 32).short("Salmão Cebola Roxa"),
		r("Atum grelhado", 29),
		r("Camarão cheese", 30),
		r("Camarão empanado", 31).short("Camarão Empanado"),
		r("Skin cheese", 26),
		r("Kani cheese", 26),
	), sized("Temaki Hot", []string{""},
		r("Hot salmão cheese", 34).short("Salmão Cheese"),
		r("Hot dinamite", 34).short("Dinamite"),
		r("Hot atum grelhado", 31).short("Atum Grelhado"),
		r("Hot camarão cheese", 32).short("Camarão Cheese"),
	)...)},
	// shortcut: 1/2 priced at half of the menu's whole price, fix in admin if different.
	{"Sushi Dog", sized("Dog", []string{"1/2", ""},
		r("Salmão cheese", 19, 38),
		r("Salmão maçaricado", 19, 38),
		r("Dinamite", 19, 38),
		r("Salmão cebola roxa", 19, 38),
		r("Camarão cheese", 18, 36),
		r("Atum grelhado", 17, 34),
		r("Kani cheese", 16, 32),
	)},
	{"Combos", []item{
		single("Combo 1", 65, "Combo 1 (22un)", "22 peças: 5 hot dinamite, 5 hot filadélfia, 4 futomaki atum grelhado, 4 uramaki camarão cheese, 2 uramaki skin cheese, 2 uramaki kani cheese"),
		single("Combo 2", 90, "Combo 2 (36un)", "36 peças: 5 hot camarão, 5 hot dinamite, 5 hot filadélfia, 4 uramaki atum grelhado, 4 futomaki camarão cheese, 4 uramaki skin cheese, 3 uramaki salmão, 2 uramaki kani cheese, 4 kani cheese"),
		single("Combo 3", 70, "Combo 3 (24un)", "24 peças: 4 uramaki salmão grelhado, 4 uramaki salmão cheese, 4 uramaki camarão cheese, 4 futomaki skin cheese, 4 futomaki atum grelhado, 4 kani cheese"),
		single("Combo Hot", 60, "Combo Hot (20un)", "20 peças: 5 filadélfia, 5 dinamite, 5 camarão cheese, 5 atum grelhado"),
		single("Combo Especial", 70, "Combo Especial (22un)", "22 peças: 5 hot dinamite, 5 hot camarão, 4 uramaki skin, 4 kani cheese, 2 jyo, 2 bora bora"),
	}},
	{"Petiscos", []item{
		single("Croquete de cupim", 25, "Croquete Cupim", "Bolinho de cupim recheado com queijo, molho da casa"),
		single("Coxinha sem massa", 25, "Coxinha s/ Massa", "Coxinha de frango sem massa com recheio de catupiry"),
		single("Bolinho de caranguejo", 30, "Bolinho Caranguejo", "Bolinho de caranguejo, catupiry e molho da casa"),
		single("Camarão empanado", 30, "Camarão Empanado", "Camarão empanado no catupiry"),
		single("Filé com fritas", 50, "Filé c/ Fritas", "Carne acebolada em tiras, batata frita ou macaxeira, farofa panko de bacon e molho da casa"),
		single("Frango crocante", 50, "Frango Crocante", "Frango empanado crocante, batata frita, molho especial e maionese da casa"),
		single("Camarão alho e óleo", 25, "Camarão Alho e Óleo", ""),
		single("Batata simples", 22, "Batata Simples", "Batata frita e molho"),
		single("Batata cheddar e bacon", 27, "Batata Cheddar Bacon", "Batata frita recheada com cheddar e bacon"),
		single("Batata carne e queijo", 27, "Batata Carne Queijo", "Batata frita recheada com queijo e carne desfiada"),
		single("Macaxeira simples", 22, "Macaxeira Simples", "Macaxeira frita e molho"),
		single("Macaxeira carne e queijo", 25, "Macaxeira, Carne e Queijo", "Macaxeira frita recheada com queijo e carne desfiada"),
		single("Macaxeira calabresa e catupiry", 25, "Macax Calabresa Catup", "Macaxeira frita recheada com calabresa e catupiry"),
	}},
	{"Bebidas", []item{
		{"Refrigerante lata", "", []product{
			{"Coca", "", "Coca Lata", 700}, {"Coca Zero", "", "Coca Zero Lata", 700},
			{"Guaraná", "", "Guaraná Lata", 700}, {"Guaraná Zero", "", "Guaraná Zero Lata", 700},
		}},
		{"Refrigerante litro", "", []product{
			{"Coca", "", "Coca Litro", 1200}, {"Coca Zero", "", "Coca Zero Litro", 1200},
			{"Guaraná", "", "Guaraná Litro", 1200}, {"Guaraná Zero", "", "Guaraná Zero Litro", 1200},
		}},
		{"Água", "", []product{{"Mineral", "", "Água Mineral", 200}, {"Com gás", "", "Água c/ Gás", 400}}},
		{"Suco", "", []product{{"Copo", "", "Suco Copo", 700}, {"Jarra", "", "Suco Jarra", 1500}}},
		single("Long neck Heineken", 12, "Heineken Long Neck", ""),
		single("H2O", 8, "H2O", ""),
	}},
}

// Menu adds the sample menu to d. It returns an error if the menu already has categories.
func Menu(ctx context.Context, d *db.DB) error {
	menu, err := d.Menu(ctx, false)
	if err != nil {
		return err
	}
	if len(menu) > 0 {
		return errors.New("menu is not empty; seed only fills an empty menu")
	}
	for pos, c := range sample {
		catID, err := d.CreateCategory(ctx, c.category, int64(pos))
		if err != nil {
			return err
		}
		for _, it := range c.items {
			itemID, err := d.CreateItem(ctx, db.Item{CategoryID: catID, Name: it.name, Description: it.description, Active: true})
			if err != nil {
				return err
			}
			for _, p := range it.products {
				if _, err := d.CreateProduct(ctx, db.Product{ItemID: itemID, Variant: p.variant, Size: p.size,
					PrintableName: p.printable, PriceCents: p.cents, Active: true}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

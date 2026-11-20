// Command seed fills an empty menu with a sample sushi menu, for trying the app out.
// Run it with `make seed`; it refuses to touch a menu that already has categories.
package main

import (
	"context"
	"errors"
	"log"
	"os"

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

var sample = []struct {
	category string
	items    []item
}{
	{"Sushi", []item{
		{"Hot roll", "10 unidades", []product{{"", "", "HOT ROLL", 1500}}},
		{"Uramaki", "8 unidades", []product{{"Salmão", "", "URAMAKI SALMAO", 2200}, {"Skin", "", "URAMAKI SKIN", 1900}}},
		{"Temaki", "", []product{{"Salmão", "", "TEMAKI SALMAO", 2800}, {"Camarão", "", "TEMAKI CAMARAO", 3200}}},
	}},
	{"Combinados", []item{
		{"Combinado", "", []product{{"", "20 peças", "COMBO 20", 6500}, {"", "40 peças", "COMBO 40", 12000}}},
	}},
	{"Refrigerante", []item{
		{"Coca-Cola", "", []product{{"", "Lata", "COCA LT", 600}, {"Zero", "Lata", "COCA ZERO LT", 600}, {"", "2L", "COCA 2L", 1400}}},
		{"Guaraná Antarctica", "", []product{{"", "Lata", "GUARANA LT", 550}}},
	}},
	{"Bebidas", []item{
		{"Água", "", []product{{"Sem gás", "", "AGUA S/GAS", 400}, {"Com gás", "", "AGUA C/GAS", 450}}},
	}},
}

func main() {
	path := os.Getenv("DB_PATH")
	if path == "" {
		path = "ukemochi.db"
	}
	d, err := db.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()
	if err := seed(context.Background(), d); err != nil {
		log.Fatal(err)
	}
	log.Printf("sample menu added to %s", path)
}

func seed(ctx context.Context, d *db.DB) error {
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

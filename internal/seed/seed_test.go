package seed

import (
	"context"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/antoniohauren/ukemochi/internal/db"
)

func TestSeed(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	if err := Menu(ctx, d); err != nil {
		t.Fatal(err)
	}
	menu, err := d.Menu(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(menu) != len(sample) {
		t.Fatalf("got %d categories, want %d", len(menu), len(sample))
	}
	for _, c := range menu {
		for _, it := range c.Items {
			for _, p := range it.Products {
				if n := utf8.RuneCountInString(p.PrintableName); n == 0 || n > 30 {
					t.Errorf("%s / %s: label %q has %d runes", c.Name, it.Name, p.PrintableName, n)
				}
				if p.PriceCents <= 0 {
					t.Errorf("%s / %s: price %d", c.Name, it.Name, p.PriceCents)
				}
			}
		}
	}
	if err := Menu(ctx, d); err == nil {
		t.Error("second seed: want error on non-empty menu")
	}
}

// Command seed fills an empty menu with the restaurant's menu.
// Run it with `make seed`; it refuses to touch a menu that already has categories.
package main

import (
	"context"
	"log"
	"os"

	"github.com/antoniohauren/ukemochi/internal/db"
	"github.com/antoniohauren/ukemochi/internal/seed"
)

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
	if err := seed.Menu(context.Background(), d); err != nil {
		log.Fatal(err)
	}
	log.Printf("menu added to %s", path)
}

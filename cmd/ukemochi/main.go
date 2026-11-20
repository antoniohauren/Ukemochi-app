package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/antoniohauren/ukemochi"
	"github.com/antoniohauren/ukemochi/internal/btprint"
	"github.com/antoniohauren/ukemochi/internal/db"
	"github.com/antoniohauren/ukemochi/internal/seed"
	"github.com/antoniohauren/ukemochi/internal/web"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// authConfig reads ADMIN_PIN and SESSION_KEY. Without SESSION_KEY a random key is used,
// so admin sessions end when the app restarts.
func authConfig() (*web.Auth, error) {
	a := &web.Auth{PIN: os.Getenv("ADMIN_PIN"), Key: []byte(os.Getenv("SESSION_KEY"))}
	switch {
	case a.PIN == "":
		log.Print("ADMIN_PIN not set: admin login is disabled")
	case len(a.PIN) < 6:
		// Any LAN device can guess; 5 tries per 15 min breaks a 4-digit PIN in days.
		return nil, errors.New("ADMIN_PIN must be at least 6 characters")
	}
	switch {
	case len(a.Key) == 0:
		log.Print("SESSION_KEY not set: using a random key, admin sessions end on restart")
		a.Key = make([]byte, 32)
		if _, err := rand.Read(a.Key); err != nil {
			return nil, err
		}
	case len(a.Key) < 32:
		return nil, errors.New("SESSION_KEY must be at least 32 characters")
	}
	return a, nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if err := loadDotenv(); err != nil {
		return err
	}
	addr := env("ADDR", "127.0.0.1:8080")
	dbPath := env("DB_PATH", "ukemochi.db")
	_, statErr := os.Stat(dbPath)
	database, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer database.Close()
	// Seed only a brand-new file, so a menu emptied on purpose stays empty.
	if errors.Is(statErr, fs.ErrNotExist) {
		if err := seed.Menu(context.Background(), database); err != nil {
			return fmt.Errorf("seed menu: %w", err)
		}
		log.Printf("new database %s: sample menu added", dbPath)
	}

	static, err := fs.Sub(ukemochi.Static, "static")
	if err != nil {
		return err
	}
	auth, err := authConfig()
	if err != nil {
		return err
	}

	mime.AddExtensionType(".webmanifest", "application/manifest+json")
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	// The service worker lives at the root so its scope covers the whole app.
	mux.HandleFunc("GET /sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, static, "sw.js")
	})
	server := &web.Server{DB: database, Auth: auth, CompactTicket: os.Getenv("TICKET_ITEMS") == "compact"}
	if mac := os.Getenv("PRINTER_BT"); mac != "" {
		server.Print = func(text string) error { return btprint.Send(mac, btprint.Ticket(text)) }
		log.Printf("printing to bluetooth printer %s", mac)
	}
	if os.Getenv("PRINT_PREVIEW") == "1" {
		server.Print = func(text string) error {
			log.Printf("print preview, not sent:\n%s", text)
			return nil
		}
		log.Print("PRINT_PREVIEW=1: tickets are logged here, not sent to the printer")
	}
	server.Routes(mux)

	srv := &http.Server{
		Addr:              addr,
		Handler:           http.NewCrossOriginProtection().Handler(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Print("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, make([]byte, maxLogSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOG_PATH", path)
	defer log.SetOutput(os.Stderr)
	f, err := setupLog()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f == nil || f.Name() != path {
		t.Fatalf("setupLog file = %v, want %q", f, path)
	}
	log.Print("hello")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "hello") || len(data) > 100 {
		t.Errorf("log file = %d bytes %q, want only the new line", len(data), data)
	}
	if fi, err := os.Stat(path + ".old"); err != nil || fi.Size() != maxLogSize+1 {
		t.Errorf("old log not kept: %v", err)
	}
}

func TestSetupLogStderrOnly(t *testing.T) {
	t.Setenv("LOG_PATH", "")
	if f, err := setupLog(); err != nil || f != nil {
		t.Errorf("setupLog = %v, %v; want stderr only", f, err)
	}
}

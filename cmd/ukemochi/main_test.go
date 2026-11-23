package main

import "testing"

func TestLocalURL(t *testing.T) {
	for addr, want := range map[string]string{
		"127.0.0.1:8080":   "http://127.0.0.1:8080",
		":8080":            "http://localhost:8080",
		"0.0.0.0:8080":     "http://localhost:8080",
		"[::]:8080":        "http://localhost:8080",
		"192.168.0.10:80":  "http://192.168.0.10:80",
		"pedidos.lan:8080": "http://pedidos.lan:8080",
	} {
		if got := localURL(addr); got != want {
			t.Errorf("localURL(%q) = %q, want %q", addr, got, want)
		}
	}
}

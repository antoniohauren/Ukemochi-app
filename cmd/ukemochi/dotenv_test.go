package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	data := "# comment\r\n\r\nUKE_A=1\r\nexport UKE_B = \"two words\"\nUKE_C='x=y'\nUKE_EMPTY=\nUKE_SET=file\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UKE_SET", "env")
	for _, k := range []string{"UKE_A", "UKE_B", "UKE_C", "UKE_EMPTY"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"UKE_A": "1", "UKE_B": "two words", "UKE_C": "x=y", "UKE_EMPTY": "", "UKE_SET": "env"} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if err := loadEnvFile(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Errorf("missing file: %v", err)
	}
	bad := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(bad, []byte("NOEQUALS\n"), 0o600)
	if err := loadEnvFile(bad); err == nil {
		t.Error("want error for line without =")
	}
}

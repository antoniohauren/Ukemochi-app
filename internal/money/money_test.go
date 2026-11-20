package money

import (
	"math"
	"testing"
)

func TestFormat(t *testing.T) {
	tests := []struct {
		cents int64
		want  string
	}{
		{0, "R$ 0,00"},
		{5, "R$ 0,05"},
		{50, "R$ 0,50"},
		{1250, "R$ 12,50"},
		{99999, "R$ 999,99"},
		{100000, "R$ 1.000,00"},
		{123456, "R$ 1.234,56"},
		{123456789, "R$ 1.234.567,89"},
		{-100, "-R$ 1,00"},
		{math.MinInt64, "-R$ 92.233.720.368.547.758,08"},
	}
	for _, tt := range tests {
		if got := Format(tt.cents); got != tt.want {
			t.Errorf("Format(%d) = %q, want %q", tt.cents, got, tt.want)
		}
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"12,50", 1250},
		{"12,5", 1250},
		{"12", 1200},
		{"0", 0},
		{",50", 50},
		{"12,", 1200},
		{"1.234,56", 123456},
		{"1.234", 123400},
		{"12.50", 1250},
		{"12.5", 1250},
		{"R$ 12,50", 1250},
		{"  R$12,50 ", 1250},
		{"1.234.567,89", 123456789},
		{"00000000001,00", 100},
		{"007", 700},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("Parse(%q) = %d, %v, want %d", tt.in, got, err, tt.want)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	for _, in := range []string{
		"", " ", "R$", "abc", "-1", "+1", "1,234", "12,5,0", "1.23.4", "12.34.567",
		"1234.567", ".", ",", "1e3", "12 50", "99999999999", "10000000,01", "١٢",
	} {
		if got, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %d, want error", in, got)
		}
	}
}

func TestChange(t *testing.T) {
	tests := []struct {
		total, given, want int64
		ok                 bool
	}{
		{3600, 5000, 1400, true},
		{3600, 3600, 0, true},
		{3600, 3500, 0, false},
		{0, 0, 0, true},
	}
	for _, tt := range tests {
		got, ok := Change(tt.total, tt.given)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Change(%d, %d) = %d, %v, want %d, %v", tt.total, tt.given, got, ok, tt.want, tt.ok)
		}
	}
}

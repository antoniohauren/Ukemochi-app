// Package money formats and parses Brazilian real amounts held as integer centavos.
package money

import (
	"errors"
	"strconv"
	"strings"
)

// ErrInvalid is returned by Parse for input that is not a valid non-negative amount.
var ErrInvalid = errors.New("valor inválido")

// maxCents caps parsed amounts at R$ 10.000.000,00, far above any order, so sums never overflow.
const maxCents = 1_000_000_000

// Format renders cents as "R$ 1.234,56". Negative amounts render as "-R$ 1,00".
func Format(cents int64) string {
	sign := ""
	u := uint64(cents)
	if cents < 0 {
		sign = "-"
		u = uint64(-cents) // wraps correctly for math.MinInt64
	}
	whole := strconv.FormatUint(u/100, 10)
	frac := u % 100

	var b strings.Builder
	b.WriteString(sign)
	b.WriteString("R$ ")
	for i, d := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(d)
	}
	b.WriteByte(',')
	b.WriteByte(byte('0' + frac/10))
	b.WriteByte(byte('0' + frac%10))
	return b.String()
}

// Parse reads a typed amount such as "12,50", "12", "1.234,56" or "R$ 12,50" into cents.
// A comma is the decimal separator. Without a comma, a single dot followed by one or two
// digits is read as a decimal point too ("12.50"), because some phone keyboards only offer a dot.
func Parse(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSpace(strings.TrimPrefix(s, "R$"))
	if s == "" {
		return 0, ErrInvalid
	}

	whole, frac := s, ""
	if i := strings.LastIndexByte(s, ','); i >= 0 {
		whole, frac = s[:i], s[i+1:]
	} else if i := strings.LastIndexByte(s, '.'); i >= 0 && strings.Count(s, ".") == 1 && len(s)-i-1 <= 2 {
		whole, frac = s[:i], s[i+1:]
	}
	if len(frac) > 2 || (whole == "" && frac == "") {
		return 0, ErrInvalid
	}

	// Dots in the whole part are thousands separators; each group after the first has 3 digits.
	if strings.Contains(whole, ".") {
		groups := strings.Split(whole, ".")
		for i, g := range groups {
			if g == "" || (i > 0 && len(g) != 3) || len(groups[0]) > 3 {
				return 0, ErrInvalid
			}
		}
		whole = strings.Join(groups, "")
	}
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	for len(frac) < 2 {
		frac += "0"
	}
	if !digits(whole) || !digits(frac) || len(whole) > 10 {
		return 0, ErrInvalid
	}

	w, _ := strconv.ParseInt(whole, 10, 64)
	f, _ := strconv.ParseInt(frac, 10, 64)
	cents := w*100 + f
	if cents > maxCents {
		return 0, ErrInvalid
	}
	return cents, nil
}

// Change returns given minus total, and false when given does not cover total.
func Change(total, given int64) (int64, bool) {
	if given < total {
		return 0, false
	}
	return given - total, true
}

func digits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

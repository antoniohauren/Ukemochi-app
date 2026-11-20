package btprint

import "testing"

func TestTicket(t *testing.T) {
	got := string(Ticket("Crédito São\n1x AÇAÍ ★"))
	want := "\x1b@\x1bt\x03Cr\x82dito S\x84o\n1x A\x80A\x8b ?\n\x1bd\x04"
	if got != want {
		t.Errorf("Ticket = %q, want %q", got, want)
	}
}

func TestSendBadAddress(t *testing.T) {
	if err := Send("not-a-mac", nil); err == nil {
		t.Error("want error for a bad address")
	}
}

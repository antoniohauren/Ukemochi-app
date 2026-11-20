// Package btprint sends tickets to an ESC/POS thermal printer over Bluetooth serial (RFCOMM).
package btprint

import (
	"sync"
	"time"
)

// timeout bounds both the connect and the write, so a printer that is off fails fast.
const timeout = 10 * time.Second

// mu serializes jobs: the printer accepts one RFCOMM connection at a time.
var mu sync.Mutex

// Send writes data to the printer at mac (for example "06:31:3D:AE:65:A7"), keeping the connection
// open for the next job.
func Send(mac string, data []byte) error {
	mu.Lock()
	defer mu.Unlock()
	return send(mac, data)
}

// Ticket turns ticket text into ESC/POS bytes: reset, code page CP860 (ESC t 3, checked on the
// BT-583), the text, then a paper feed.
func Ticket(text string) []byte {
	out := []byte("\x1b@\x1bt\x03")
	for _, r := range text {
		switch b, ok := cp860[r]; {
		case r < 128:
			out = append(out, byte(r))
		case ok:
			out = append(out, b)
		default:
			out = append(out, '?')
		}
	}
	return append(out, "\n\x1bd\x04"...)
}

// cp860 maps runes to CP860 bytes 0x80-0xFF. One byte per rune keeps line widths as laid out.
var cp860 = func() map[rune]byte {
	m := map[rune]byte{}
	for i, r := range []rune("ÇüéâãàÁçêÊèÍÔìÃÂÉÀÈôõòÚùÌÕÜ¢£Ù₧ÓáíóúñÑªº¿Ò¬½¼¡«»░▒▓│┤╡╢╖╕╣║╗╝╜╛┐└┴┬├─┼╞╟╚╔╩╦╠═╬╧╨╤╥╙╘╒╓╫╪┘┌█▄▌▐▀αßΓπΣσµτΦΘΩδ∞φε∩≡±≥≤⌠⌡÷≈°∙·√ⁿ²■\u00a0") {
		m[r] = byte(0x80 + i)
	}
	return m
}()

package via

import (
	"fmt"
	"strings"
)

// QMK keycodes: the basic ones are HID keyboard usages, with modifier bits
// above them (Ctrl 0x100, Shift 0x200, Alt 0x400, GUI 0x800; 0x1000 makes
// them the right-hand ones). These are the same in every VIA protocol; layer
// keys moved with ProtocolV3.

const (
	KeyNone        = 0x0000
	KeyTransparent = 0x0001 // the key of the layer below
	modsShift      = 8
	rightMods      = 0x10
)

// Momentary reports the layer a key holds while pressed (QMK's MO(n)).
func Momentary(code uint16, protocol int) (int, bool) {
	if protocol >= ProtocolV3 {
		return int(code & 0x1F), code&^0x1F == 0x5220
	}
	return int(code & 0xFF), code&^0xFF == 0x5100
}

// Basic splits a key with left-hand modifiers into them (Ctrl 1, Shift 2,
// Alt 4, GUI 8) and its HID usage; ok is false for any other keycode.
func Basic(code uint16) (mods int, usage byte, ok bool) {
	m := int(code >> modsShift)
	if code >= 0x2000 || m&rightMods != 0 || code == KeyTransparent {
		return 0, 0, false
	}
	if _, media := consumer[byte(code)]; media && m == 0 {
		return 0, 0, false
	}
	return m, byte(code), true
}

// BasicCode is the keycode of a HID usage with left-hand modifiers.
func BasicCode(mods int, usage byte) uint16 { return uint16(mods&0x0F)<<modsShift | uint16(usage) }

// consumer maps QMK's media keys to the HID consumer usages they send.
var consumer = map[byte]int{
	0xA8: 0xE2, 0xA9: 0xE9, 0xAA: 0xEA, 0xAB: 0xB5, 0xAC: 0xB6, 0xAD: 0xB7, 0xAE: 0xCD, // mute, volume, tracks, stop, play
	0xBD: 0x6F, 0xBE: 0x70, // screen brightness
}

// Media reports the consumer usage a media key sends.
func Media(code uint16) (int, bool) {
	if code > 0xFF {
		return 0, false
	}
	u, ok := consumer[byte(code)]
	return u, ok
}

// MediaCode is the keycode sending a consumer usage, 0 if QMK has none.
func MediaCode(usage int) uint16 {
	for k, u := range consumer {
		if u == usage {
			return uint16(k)
		}
	}
	return 0
}

// keyNames are short labels of HID keyboard usages, for drawing keys.
var keyNames = func() map[byte]string {
	m := map[byte]string{
		0x28: "Enter", 0x29: "Esc", 0x2A: "Backspace", 0x2B: "Tab", 0x2C: "Space", 0x2D: "-", 0x2E: "=",
		0x2F: "[", 0x30: "]", 0x31: "\\", 0x32: "#", 0x33: ";", 0x34: "'", 0x35: "`", 0x36: ",", 0x37: ".",
		0x38: "/", 0x39: "CapsLock", 0x46: "PrtSc", 0x47: "ScrLk", 0x48: "Pause", 0x49: "Insert", 0x4A: "Home",
		0x4B: "PgUp", 0x4C: "Delete", 0x4D: "End", 0x4E: "PgDn", 0x4F: "Right", 0x50: "Left", 0x51: "Down",
		0x52: "Up", 0x53: "NumLock", 0x54: "Num/", 0x55: "Num*", 0x56: "Num-", 0x57: "Num+", 0x58: "NumEnter",
		0x63: "Num.", 0x64: "\\|", 0x65: "Menu", 0xE0: "LCtrl", 0xE1: "LShift", 0xE2: "LAlt", 0xE3: "LWin",
		0xE4: "RCtrl", 0xE5: "RShift", 0xE6: "RAlt", 0xE7: "RWin",
	}
	for i := range 26 {
		m[byte(0x04+i)] = string(rune('A' + i))
	}
	for i := range 9 {
		m[byte(0x1E+i)] = string(rune('1' + i))
		m[byte(0x59+i)] = fmt.Sprintf("Num%d", i+1)
	}
	m[0x27], m[0x62] = "0", "Num0"
	for i := range 12 {
		m[byte(0x3A+i)] = fmt.Sprintf("F%d", i+1)
		m[byte(0x68+i)] = fmt.Sprintf("F%d", i+13)
	}
	return m
}()

var mediaNames = map[byte]string{0xA8: "Mute", 0xA9: "Vol+", 0xAA: "Vol-", 0xAB: "Next", 0xAC: "Prev", 0xAD: "Stop", 0xAE: "Play", 0xBD: "Bri+", 0xBE: "Bri-"}

// Label is a short name for a keycode, to draw on its key.
func Label(code uint16, protocol int) string {
	if l, ok := Momentary(code, protocol); ok {
		if l == 1 {
			return "Fn"
		}
		return fmt.Sprintf("Fn%d", l)
	}
	switch {
	case code == KeyNone:
		return ""
	case code == KeyTransparent:
		return "▽"
	case code <= 0xFF && mediaNames[byte(code)] != "":
		return mediaNames[byte(code)]
	}
	mods, u, ok := Basic(code)
	if !ok || keyNames[u] == "" {
		return fmt.Sprintf("0x%04X", code)
	}
	var b strings.Builder
	for i, n := range []string{"Ctrl+", "Shift+", "Alt+", "Win+"} {
		if mods&(1<<i) != 0 {
			b.WriteString(n)
		}
	}
	return b.String() + keyNames[u]
}

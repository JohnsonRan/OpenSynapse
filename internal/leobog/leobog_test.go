package leobog

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

// Recorded from LEOBOG ONE and a Hi75: the config it read while the keyboard
// was on breathing, the block it wrote to switch to static (colorful), and
// the palette it sent after it, with blue picked for static.
var (
	recRead    = "06840000010080000003030200000404070002200100000000000100020100ff0400000001000301000000000000000000000000000000000000000000000000ffff04470440044704470447044704470447044704470447044704470447044704470447044704470437074707470744074407440744074407440744040304040404040404045aa5"
	recWrite   = "06040000010080000003030200000404070001200100000000000100020100ff0400000001000301000000000000000000000000000000000000000000000000ffff04470440044704470447044704470447044704470447044704470447044704470447044704470437074707470744074407440744074407440744040304040404040404045aa5"
	recPalette = "060a0000010000020000000000000000000000000000000000000000000000ff00ff000000ffffff00ff00ff00ffffffffff" + strings.Repeat("ff000000ff000000ffffff00ff00ff00ffffffffff", 12)
)

func report(t *testing.T, s string, tail ...byte) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, make([]byte, reportLen-len(b))...)
	copy(b[offPaletteEnd:], tail)
	return b
}

var blue = razer.RGB{B: 0xff}

func TestDecode(t *testing.T) {
	c := DefaultColors()
	c[2] = blue
	got := decode(report(t, recRead), &c, 2)
	if got != (Lighting{Effect: 2, Brightness: 4, Speed: 4, Color: blue}) {
		t.Fatalf("got %+v", got)
	}
	if got := decode(report(t, recRead), &c, 1); got != (Lighting{Effect: 1, Brightness: 4, Speed: 4, Colorful: true}) {
		t.Fatalf("static: got %+v", got)
	}
}

func TestEncodeMatchesDriver(t *testing.T) {
	w := encode(report(t, recRead), Lighting{Effect: 1, Brightness: 4, Speed: 4, Colorful: true})
	if !bytes.Equal(w, report(t, recWrite)) {
		t.Fatalf("got % x", w[:136])
	}
}

func TestPaletteMatchesDriver(t *testing.T) {
	c := DefaultColors()
	c[1] = blue
	if w := palette(&c); !bytes.Equal(w, report(t, recPalette, magic...)) {
		t.Fatalf("got % x", w[:304])
	}
}

// fakePort answers reads like the recorded keyboard and records writes.
type fakePort struct {
	t       *testing.T
	sent    [][]byte
	pending []byte
}

const recHello = "068201000100060003000000009b"

func (f *fakePort) set(r []byte) error {
	switch r[1] {
	// reads answer only the exact requests the driver sends
	case cmdHello:
		f.pending = report(f.t, "068200000100060000100000")
		if bytes.Equal(r, report(f.t, recHello[:14])) {
			f.pending = report(f.t, recHello)
		}
	case cmdConfig | readFlag:
		if !bytes.Equal(r, report(f.t, recRead[:14])) {
			f.t.Fatalf("config read % x", r[:8])
		}
		f.pending = report(f.t, recRead)
	default:
		f.sent = append(f.sent, r)
	}
	return nil
}

func (f *fakePort) get() ([]byte, error) { return append([]byte(nil), f.pending...), nil }

func TestSetLightingKeepsOtherEffects(t *testing.T) {
	f := &fakePort{t: t}
	d := &Device{port: f, colors: DefaultColors()}
	if err := d.identify(); err != nil {
		t.Fatal(err)
	}
	red := razer.RGB{R: 0xff}
	if err := d.SetLighting(Lighting{Effect: 1, Brightness: 1, Speed: 0, Color: red}); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 2 || f.sent[0][1] != cmdConfig || f.sent[1][1] != cmdPalette {
		t.Fatalf("sent %d reports", len(f.sent))
	}
	w, want := f.sent[0], report(t, recRead)
	want[1], want[offEffect], want[66], want[67] = cmdConfig, 1, 1, 0x00
	if !bytes.Equal(w, want) {
		t.Fatalf("config % x", w[:136])
	}
	if d.Colors()[1] != red || f.sent[1][offPalette+21*3] != 0xff {
		t.Fatal("picked color not kept")
	}
	if err := d.SetLighting(Lighting{Effect: 16, Brightness: 4}); err == nil {
		t.Fatal("waterfall took a single color")
	}
	if err := d.SetLighting(Lighting{Effect: 2, Brightness: 5, Colorful: true}); err == nil {
		t.Fatal("brightness 5 accepted")
	}
}

func TestUnknownFirmwareRefused(t *testing.T) {
	d := &Device{port: &fakePort{t: t}}
	old := hello
	hello = append([]byte(nil), hello...)
	hello[13] = 0x9c
	defer func() { hello = old }()
	if d.identify() == nil {
		t.Fatal("accepted another firmware")
	}
}

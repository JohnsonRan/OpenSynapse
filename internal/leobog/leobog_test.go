package leobog

import (
	"bytes"
	"encoding/hex"
	"errors"
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
	busy    int // reads still answered with the last write, as while storing it
	refuse  int // writes refused, as while storing the one before
}

const recHello = "068201000100060003000000009b"

func (f *fakePort) set(r []byte) error {
	if f.refuse > 0 && r[1]&readFlag == 0 {
		f.refuse--
		return errors.New("device not functioning")
	}
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
		if f.busy > 0 {
			f.busy--
			f.pending = f.sent[len(f.sent)-1]
		}
	default:
		f.sent = append(f.sent, r)
	}
	return nil
}

func (f *fakePort) get() ([]byte, error) { return append([]byte(nil), f.pending...), nil }

func TestSetLightingKeepsOtherEffects(t *testing.T) {
	f := &fakePort{t: t}
	d := &Device{Model: hi75, port: f, colors: DefaultColors()}
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
	other := *hi75
	other.Hello = append([]byte(nil), hi75.Hello...)
	other.Hello[13] = 0x9c
	d := &Device{Model: &other, port: &fakePort{t: t}}
	if d.identify() == nil {
		t.Fatal("accepted another firmware")
	}
}

// The driver's per-key report with Esc red, F1 green, ` blue and Space white.
func TestKeyReportMatchesDriver(t *testing.T) {
	var c KeyColors
	c[0], c[12], c[1], c[35] = razer.RGB{R: 0xff}, razer.RGB{G: 0xff}, blue, razer.RGB{R: 0xff, G: 0xff, B: 0xff}
	w := keyReport(cmdKeys, &c)
	want := report(t, "060600000100"+"7a01")
	for _, i := range []int{8, 43, 146, 169, 261, 295} {
		want[i] = 0xff
	}
	if !bytes.Equal(w, want) {
		t.Fatalf("got % x", w[:300])
	}
}

// Custom switches the config's custom flag; any other effect clears it.
func TestEncodeCustomFlag(t *testing.T) {
	cfg := report(t, recRead)
	w := encode(cfg, Lighting{Effect: EffectCustom, Brightness: 2})
	if w[offCustom] != 1 || w[offEffect] != EffectCustom || !bytes.Equal(w[offParams:offMagic], cfg[offParams:offMagic]) {
		t.Fatalf("custom: % x", w[16:20])
	}
	if w := encode(w, Lighting{Effect: 12, Brightness: 4, Speed: 4, Colorful: true}); w[offCustom] != 0 || w[offEffect] != 12 {
		t.Fatalf("leaving custom: % x", w[16:20])
	}
}

// The driver's default layer with CapsLock as left Ctrl (CapsLock is index 3).
func TestKeymapReportMatchesDriver(t *testing.T) {
	l := hi75.DefaultLayers()
	l[0][3] = Action{Type: ActionKey, Mods: 1}
	w := keymapReport(0, &l[0])
	want := report(t, "0603000001000002"+hi75.defaultLayers[0])
	copy(want[8+4*3:], []byte{0, 1, 0, 0})
	copy(want[reportLen-2:], magic)
	if !bytes.Equal(w, want) {
		t.Fatalf("got % x", w[:24])
	}
	if w := keymapReport(1, &l[1]); w[2] != 1 || actionOf(w[8+4*8:]) != (Action{Type: 7, Code: 0x19}) {
		t.Fatalf("Fn layer: % x", w[:48])
	}
	if l[0][9] != (Action{Type: ActionKey, Code: 0x04}) || l[1][len(l[1])-1] != (Action{}) {
		t.Fatal("default A is not A")
	}
}

// A recorded music frame: Left Shift dim green, Left Ctrl green, A dark blue.
func TestFrameMatchesDriver(t *testing.T) {
	var c KeyColors
	c[4], c[5], c[9] = razer.RGB{G: 0x8f}, razer.RGB{G: 0xff}, razer.RGB{G: 0x0c, B: 0x52}
	w := keyReport(cmdFrame, &c)
	if !bytes.Equal(w[:8], []byte{6, 8, 0, 0, 1, 0, 0x7a, 1}) || !bytes.Equal(w[8+3*4:8+3*10], []byte{0, 0x8f, 0, 0, 0xff, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x0c, 0x52}) {
		t.Fatalf("got % x", w[:40])
	}
}

// The driver's factory reset, as recorded: four keymap reports (the last two
// empty), the config with effect 11 and the factory settings, the default
// palette, and the game keys white.
func TestResetMatchesDriver(t *testing.T) {
	noResetPauses(t)
	f := &fakePort{t: t, refuse: resetTries - 1} // the first write goes through on its last try
	d := &Device{Model: hi75, port: f}
	if err := d.Reset(); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 7 {
		t.Fatalf("sent %d reports", len(f.sent))
	}
	for i, r := range f.sent[:4] {
		if r[1] != cmdKeymap || r[2] != byte(i) || !bytes.Equal(r[reportLen-2:], magic) {
			t.Fatalf("keymap %d: % x", i, r[:8])
		}
	}
	if bytes.ContainsFunc(f.sent[3][8:reportLen-2], func(r rune) bool { return r != 0 }) {
		t.Fatal("layer 3 is not empty")
	}
	cfg := f.sent[4]
	// The block the driver wrote, header to magic; the rest is zero.
	const recWrite = "0604000001008000000303020000040407000b200100000000000100020100ff0400000001000301000000000000000000000000000000000000000000000000ffff0447044704470447044704470447044704470447044704470447044704470447044704470447043707470747074407440744074407440744074404030404040404040404" + "5aa5"
	if !bytes.Equal(cfg, report(t, recWrite)) {
		t.Fatalf("config % x", cfg[8:48])
	}
	colors := DefaultColors()
	if !bytes.Equal(f.sent[5], palette(&colors)) {
		t.Fatal("palette is not the default")
	}
	k := f.sent[6]
	if k[1] != cmdKeys || k[offRed+9] != 0xff || k[offBlue+89] != 0xff || k[offGreen+1] != 0 {
		t.Fatal("factory key colors")
	}
	if d.KeyColors() != hi75.FactoryKeyColors() || d.Colors() != DefaultColors() {
		t.Fatal("device kept the old colors")
	}
}

// Busy storing a write, the keyboard answers reads with it for a while; the
// read is asked again rather than failing the call after a reset.
func TestReadWaitsWhileBusy(t *testing.T) {
	orig := busyWait
	busyWait = 0
	t.Cleanup(func() { busyWait = orig })
	f := &fakePort{t: t, sent: [][]byte{report(t, "0603030001000002")}}
	d := &Device{Model: hi75, port: f}
	f.busy = busyTries - 1
	if _, err := d.Lighting(); err != nil {
		t.Fatal(err)
	}
	f.busy = busyTries
	if _, err := d.Lighting(); err == nil {
		t.Fatal("accepted a stale answer")
	}
}

func noResetPauses(t *testing.T) {
	gap, settle := resetGap, resetSettle
	resetGap, resetSettle = 0, 0
	t.Cleanup(func() { resetGap, resetSettle = gap, settle })
}

// A write refused every time stops the reset with the keyboard's error.
func TestResetGivesUpOnRefusedWrite(t *testing.T) {
	noResetPauses(t)
	f := &fakePort{t: t, refuse: resetTries}
	d := &Device{Model: hi75, port: f}
	if err := d.Reset(); err == nil || len(f.sent) != 0 {
		t.Fatalf("err %v, sent %d", err, len(f.sent))
	}
}

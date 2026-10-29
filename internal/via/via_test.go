package via

import (
	"encoding/binary"
	"testing"
)

// fakeKeyboard answers like QMK's via.c: a keymap in an EEPROM-like buffer
// and lighting values by channel and ID.
type fakeKeyboard struct {
	t      *testing.T
	m      *Model
	keymap []byte // layers × rows × cols, big-endian
	values map[[2]byte][]byte
	saved  []byte // channels saved
	in     chan []byte
}

func newFake(t *testing.T, m *Model, layers int) *fakeKeyboard {
	return &fakeKeyboard{t: t, m: m, keymap: make([]byte, 2*layers*m.Rows*m.Cols), values: map[[2]byte][]byte{}, in: make(chan []byte, 4)}
}

func (f *fakeKeyboard) send(r []byte) error {
	if len(r) != ReportLen {
		f.t.Fatalf("report of %d bytes", len(r))
	}
	a := append([]byte(nil), r...)
	switch r[0] {
	case cmdProtocol:
		a[1], a[2] = 0, ProtocolV3
	case cmdLayerCount:
		a[1] = byte(len(f.keymap) / (2 * f.m.Rows * f.m.Cols))
	case cmdKeymapRead:
		off, size := int(r[1])<<8|int(r[2]), int(r[3])
		copy(a[4:], f.keymap[off:off+size])
	case cmdSetKeycode:
		i := 2 * ((int(r[1])*f.m.Rows+int(r[2]))*f.m.Cols + int(r[3]))
		copy(f.keymap[i:], r[4:6])
	case cmdGetKeycode:
		i := 2 * ((int(r[1])*f.m.Rows+int(r[2]))*f.m.Cols + int(r[3]))
		copy(a[4:6], f.keymap[i:])
	case cmdSetValue:
		f.values[[2]byte{r[1], r[2]}] = append([]byte(nil), r[3:5]...)
	case cmdGetValue:
		copy(a[3:], f.values[[2]byte{r[1], r[2]}])
	case cmdSaveValues:
		f.saved = append(f.saved, r[1])
	case cmdResetKeymap:
		clear(f.keymap)
	default:
		a[0] = cmdUnhandled
	}
	f.in <- a
	return nil
}

func (f *fakeKeyboard) replies() <-chan []byte { return f.in }
func (f *fakeKeyboard) close()                 {}

var model = &Model{Name: "Test 60", Rows: 5, Cols: 15, Light: &Lighting{
	Brightness: &Value{3, 1, 255}, Effect: &Value{3, 2, 255}, Speed: &Value{3, 3, 255}, Color: &Value{3, 4, 255},
	Effects: []Effect{{0, "All Off"}, {1, "Solid Color"}, {2, "Breathing"}},
}}

func TestKeymapAndLighting(t *testing.T) {
	f := newFake(t, model, 4)
	binary.BigEndian.PutUint16(f.keymap[2*(5*15+4*15+3):], 0x5221) // layer 1, row 4, col 3: MO(1)
	d := &Device{Model: model, port: f}
	if err := d.open(); err != nil || d.Protocol != ProtocolV3 || d.Layers != 4 {
		t.Fatalf("open: %v, protocol %d, %d layers", err, d.Protocol, d.Layers)
	}
	if err := d.SetKeycode(0, 2, 14, BasicCode(1, 0x04)); err != nil {
		t.Fatal(err)
	}
	km, err := d.Keymap(0)
	if err != nil || len(km) != 75 || km[model.Index(2, 14)] != 0x0104 {
		t.Fatalf("layer 0: %v % x", err, km)
	}
	if c, err := d.Keycode(1, 4, 3); err != nil || c != 0x5221 {
		t.Fatalf("keycode %04x %v", c, err)
	}
	if km, _ := d.Keymap(1); km[model.Index(4, 3)] != 0x5221 {
		t.Fatal("layer 1 read from the wrong offset")
	}
	if err := d.SetLighting(State{Effect: 2, Brightness: 200, Speed: 9, Hue: 85, Sat: 255}); err != nil {
		t.Fatal(err)
	}
	if s, err := d.Lighting(); err != nil || s != (State{Effect: 2, Brightness: 200, Speed: 9, Hue: 85, Sat: 255}) {
		t.Fatalf("lighting %+v %v", s, err)
	}
	if len(f.saved) != 1 || f.saved[0] != 3 {
		t.Fatalf("saved channels %v", f.saved)
	}
	if d.SetLighting(State{Effect: 7}) == nil || d.SetKeycode(4, 0, 0, 0) == nil {
		t.Fatal("accepted an unknown effect or layer")
	}
}

func TestKeycodes(t *testing.T) {
	if l, ok := Momentary(0x5221, ProtocolV3); !ok || l != 1 || Label(0x5101, 11) != "Fn" {
		t.Fatal("MO(1)")
	}
	if m, u, ok := Basic(0x0304); !ok || m != 3 || u != 0x04 || Label(0x0304, 12) != "Ctrl+Shift+A" {
		t.Fatal("Ctrl+Shift+A")
	}
	if _, _, ok := Basic(0x1104); ok {
		t.Fatal("right Ctrl taken as left")
	}
	if u, ok := Media(0xA9); !ok || u != 0xE9 || MediaCode(0xE9) != 0xA9 {
		t.Fatal("volume up")
	}
	if _, _, ok := Basic(0xA9); ok {
		t.Fatal("volume up taken as a key")
	}
}

func TestCatalog(t *testing.T) {
	ms := Models()
	if len(ms) < 1000 {
		t.Fatalf("%d keyboards", len(ms))
	}
	for id, m := range ms {
		if m.Rows < 1 || m.Cols < 1 || len(m.Keys) == 0 {
			t.Fatalf("%04x:%04x %s", id[0], id[1], m.Name)
		}
		for _, k := range m.Keys {
			if k.Row >= m.Rows || k.Col >= m.Cols || k.X < 0 || k.Y < 0 || k.X+k.W > m.Width || k.Y+k.H > m.Height {
				t.Fatalf("%s: key %+v in %dx%d", m.Name, k, m.Width, m.Height)
			}
		}
	}
	if m := ms[[2]uint16{0x3434, 0x0100}]; m == nil || m.Light == nil || m.Light.Effect == nil || len(m.Light.Effects) < 10 {
		t.Fatalf("Keychron Q1: %+v", m)
	}
}

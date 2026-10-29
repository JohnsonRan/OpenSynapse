package main

import (
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
	"github.com/JohnsonRan/OpenSynapse/internal/via"
)

func TestViaColors(t *testing.T) {
	for _, c := range []razer.RGB{{R: 255}, {G: 255}, {B: 255}, {R: 255, G: 255, B: 255}, {R: 255, B: 255}} {
		h, s := rgbToHS(c)
		// QMK's hue has 256 steps: colors between them come back a few levels off.
		near := func(a, b byte) bool { return max(a, b)-min(a, b) <= 4 }
		if got := hsvToRGB(h, s); !near(got.R, c.R) || !near(got.G, c.G) || !near(got.B, c.B) {
			t.Fatalf("%+v → %d,%d → %+v", c, h, s, got)
		}
	}
	if h, s := rgbToHS(razer.RGB{G: 255}); h != 85 || s != 255 {
		t.Fatalf("green is hue %d", h)
	}
}

func TestViaLevels(t *testing.T) {
	v := &via.Value{Max: 100}
	if toLevel(v, 100) != viaMaxLevel || fromLevel(v, viaMaxLevel) != 100 || fromLevel(v, toLevel(v, 37)) != 37 {
		t.Fatal("scaling")
	}
}

// Page actions and keycodes go both ways.
func TestViaKeyActions(t *testing.T) {
	for _, act := range []ButtonAction{
		{Kind: "key", Code: 0x41, Mods: 1},  // Ctrl+A
		{Kind: "key", Code: 0x7B},           // F12
		{Kind: "media", Code: 0xE9},         // volume up
		{Kind: "key", Code: 0, Mods: 2 | 4}, // Shift+Alt alone
	} {
		code, err := viaCodeFor(act)
		if err != nil {
			t.Fatal(act, err)
		}
		if got := viaKeyAction(code, 0); act.Code != 0 && got != act {
			t.Fatalf("%+v → %04x → %+v", act, code, got)
		}
	}
	if c, _ := viaCodeFor(ButtonAction{Kind: "key", Code: 0, Mods: 2 | 4}); c != 0x04E1 {
		t.Fatalf("Shift+Alt is %04x", c)
	}
	if _, err := viaCodeFor(ButtonAction{Kind: "key", Code: firstTrigger}); err == nil {
		t.Fatal("a macro trigger accepted as a key")
	}
	if triggerCode(firstTrigger+11) != 0x73 || viaKeyAction(0x5221, 4).Kind != "other" || viaKeyAction(4, 4).Kind != "default" {
		t.Fatal("trigger, Fn or default")
	}
}

// The page's speed runs slow to fast; EVision's from 5 (slowest) to 0.
func TestEVisionSpeed(t *testing.T) {
	if evisionSpeed(0) != 5 || evisionSpeed(4) != 0 || evisionLevel(5) != 0 || evisionLevel(0) != 4 {
		t.Fatal("ends")
	}
	for l := range 5 {
		if evisionLevel(evisionSpeed(l)) != l {
			t.Fatalf("level %d → %d → %d", l, evisionSpeed(l), evisionLevel(evisionSpeed(l)))
		}
	}
}

type fakeViaPort struct {
	in chan []byte
}

func (p *fakeViaPort) Send([]byte) error      { return nil }
func (p *fakeViaPort) Replies() <-chan []byte { return p.in }
func (p *fakeViaPort) Close()                 {}

func TestViaModelsAndUnsure(t *testing.T) {
	m1 := &via.Model{Name: "Model 1", Rows: 5, Cols: 15}
	m2 := &via.Model{Name: "Model 2", Rows: 5, Cols: 15}
	p := &fakeViaPort{in: make(chan []byte)}
	d := via.NewDevice(m1, p)
	d.ID = "1234:5678"
	d.Variants = []*via.Model{m1, m2}
	d.Unsure = true

	viaKeyboards.mu.Lock()
	viaKeyboards.devs = []*via.Device{d}
	viaKeyboards.mu.Unlock()
	t.Cleanup(func() {
		viaKeyboards.mu.Lock()
		viaKeyboards.devs = nil
		viaKeyboards.mu.Unlock()
	})

	vm, err := (Via{}).Models(d.ID)
	if err != nil || len(vm.Models) != 2 || !vm.Unsure || vm.Model != "Model 1" {
		t.Fatalf("Models: %+v, %v", vm, err)
	}

	if _, err := (Via{}).State(d.ID); err == nil || err.Error() != tr("err.pickModel") {
		t.Fatalf("expected pickModel error, got %v", err)
	}
	if _, err := (Via{}).Keys(d.ID); err == nil || err.Error() != tr("err.pickModel") {
		t.Fatalf("expected pickModel error, got %v", err)
	}

	if err := (Via{}).SetModel(d.ID, "Model 2"); err != nil {
		t.Fatal(err)
	}
	vm2, err := (Via{}).Models(d.ID)
	if err != nil || vm2.Unsure || vm2.Model != "Model 2" {
		t.Fatalf("Models after SetModel: %+v, %v", vm2, err)
	}
}

func TestRememberOriginal(t *testing.T) {
	cfgMu.Lock()
	cfg.ViaKeys = nil
	cfgMu.Unlock()

	if !rememberOriginal("dev1", 0, 1, 0x0004) {
		t.Fatal("first remember should return true")
	}
	if rememberOriginal("dev1", 0, 1, 0x0004) {
		t.Fatal("second remember should return false")
	}
	orig, ok := viaOriginal("dev1", 0, 1)
	if !ok || orig != 0x0004 {
		t.Fatalf("got %04x, %v", orig, ok)
	}
}

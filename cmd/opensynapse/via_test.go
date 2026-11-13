package main

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
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
	t.Setenv("APPDATA", t.TempDir())
	saved := cfg
	defer func() { cfg = saved }()

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

// Changed keys are kept by row*Cols+col. A model with another matrix width
// reads the index as another key, and one that lacks the key cannot restore it.
func TestViaSetModelKeepsChangedKeysInPlace(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	cfgMu.Lock()
	saved := cfg
	cfg = defaultConfig()
	cfgMu.Unlock()
	numpad := &via.Model{Name: "Numpad", Rows: 5, Cols: 4, Keys: []via.Key{{Row: 1, Col: 1}}}
	wide := &via.Model{Name: "Wide", Rows: 8, Cols: 16, Keys: []via.Key{{Row: 0, Col: 5}}} // index 5 too, another key
	bare := &via.Model{Name: "Bare", Rows: 5, Cols: 4, Keys: []via.Key{{Row: 0, Col: 0}}}
	same := &via.Model{Name: "Same", Rows: 5, Cols: 4, Keys: []via.Key{{Row: 1, Col: 1}, {Row: 0, Col: 0}}}
	d := via.NewDevice(numpad, &fakeViaPort{in: make(chan []byte)})
	d.ID = "1234:5678"
	d.Variants = []*via.Model{numpad, wide, bare, same}
	viaKeyboards.mu.Lock()
	viaKeyboards.devs = []*via.Device{d}
	viaKeyboards.mu.Unlock()
	t.Cleanup(func() {
		viaKeyboards.mu.Lock()
		viaKeyboards.devs = nil
		viaKeyboards.mu.Unlock()
		cfgMu.Lock()
		cfg = saved
		cfgMu.Unlock()
	})
	model := func() string {
		vm, err := (Via{}).Models(d.ID)
		if err != nil {
			t.Fatal(err)
		}
		return vm.Model
	}
	index := numpad.Index(1, 1)
	set := func(bindings []Binding, originals map[string]uint16) {
		cfgMu.Lock()
		cfg.Bindings, cfg.ViaKeys = bindings, map[string]map[string]uint16{d.ID: originals}
		cfgMu.Unlock()
	}

	for name, state := range map[string]func(){
		"a macro key":           func() { set([]Binding{{Family: viaFamily(d), Button: index, Trigger: firstTrigger}}, nil) },
		"a remembered original": func() { set(nil, map[string]uint16{viaKeyOf(1, index): 4}) },
	} {
		state()
		for _, other := range []string{"Wide", "Bare"} {
			if err := (Via{}).SetModel(d.ID, other); err == nil || err.Error() != tr("err.modelKeys") || model() != "Numpad" {
				t.Fatalf("with %s, SetModel(%s): %v, model %s; want it refused", name, other, err, model())
			}
		}
		cfgMu.Lock()
		picked := cfg.ViaModels[d.ID]
		cfgMu.Unlock()
		if picked != "" {
			t.Fatalf("with %s, a refused model %q was saved", name, picked)
		}
	}
	if err := (Via{}).SetModel(d.ID, "Same"); err != nil || model() != "Same" {
		t.Fatalf("a model with the changed key in place: %v, model %s", err, model())
	}
	set(nil, nil)
	if err := (Via{}).SetModel(d.ID, "Wide"); err != nil || model() != "Wide" {
		t.Fatalf("without changed keys: %v, model %s", err, model())
	}
}

func TestRememberOriginal(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	saved := cfg
	defer func() { cfg = saved }()

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

type failingSetKeyPort struct {
	in chan []byte
}

func (p *failingSetKeyPort) Send(r []byte) error {
	if len(r) > 0 && r[0] == 0x04 { // cmdGetKeycode
		a := append([]byte(nil), r...)
		a[4], a[5] = 0x00, 0x04 // keycode 0x0004
		p.in <- a
		return nil
	}
	if len(r) > 0 && r[0] == 0x05 { // cmdSetKeycode
		return errors.New("hardware write failed")
	}
	return nil
}

func (p *failingSetKeyPort) Replies() <-chan []byte { return p.in }
func (p *failingSetKeyPort) Close()                 {}

func TestViaSetKeyMacroFailureRollback(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	saved := cfg
	defer func() { cfg = saved }()

	cfgMu.Lock()
	cfg = config{
		Macros: []Macro{{ID: "macro1", Name: "Macro 1"}},
	}
	cfgMu.Unlock()

	m := &via.Model{Name: "Model 1", Rows: 5, Cols: 15, Keys: []via.Key{{Row: 0, Col: 0}}}
	p := &failingSetKeyPort{in: make(chan []byte, 4)}
	d := via.NewDevice(m, p)
	d.ID = "test:device"
	d.Layers = 4

	viaKeyboards.mu.Lock()
	viaKeyboards.devs = []*via.Device{d}
	viaKeyboards.mu.Unlock()
	t.Cleanup(func() {
		viaKeyboards.mu.Lock()
		viaKeyboards.devs = nil
		viaKeyboards.mu.Unlock()
	})

	err := (Via{}).SetKey(d.ID, 0, 0, ButtonAction{Kind: "macro", Macro: "macro1"})
	if err == nil {
		t.Fatal("expected SetKey to fail when SetKeycode fails")
	}

	cfgMu.Lock()
	memBindings := slices.Clone(cfg.Bindings)
	cfgMu.Unlock()
	if len(memBindings) != 0 {
		t.Fatalf("memory bindings not rolled back: %+v", memBindings)
	}

	b, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	var diskCfg config
	if err := json.Unmarshal(b, &diskCfg); err != nil {
		t.Fatalf("unmarshal disk config: %v", err)
	}
	if len(diskCfg.Bindings) != 0 {
		t.Fatalf("disk bindings not rolled back: %+v", diskCfg.Bindings)
	}
	if orig, ok := diskCfg.ViaKeys[d.ID]["0:0"]; !ok || orig != 0x0004 {
		t.Fatalf("expected captured original on disk, got %04x (ok=%v)", orig, ok)
	}
}

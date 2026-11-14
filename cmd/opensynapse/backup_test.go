package main

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
	"github.com/JohnsonRan/OpenSynapse/internal/razer"
	"github.com/JohnsonRan/OpenSynapse/internal/via"
)

// An imported binding reaches the device as it is, so import keeps only what
// SetButton and SetKey would have made.
func TestImportBindings(t *testing.T) {
	macros := []Macro{{ID: "m"}}
	kb := leobog.Models[0]
	key := slices.IndexFunc(kb.Layout, func(k leobog.Key) bool { return k.Index != kb.FnIndex })
	noKey := 0
	for slices.ContainsFunc(kb.Layout, func(k leobog.Key) bool { return k.Index == noKey }) {
		noKey++
	}
	side := razer.Assignment{Fn: razer.FnMouse, Data: []byte{4}}
	trig := func(i int) int { return firstTrigger + i }
	in := []Binding{
		{Family: "Mouse", Button: 4, Macro: "m", Trigger: trig(0), Original: side},
		{Family: "Mouse", Button: leftButton, Layer: razer.LayerNormal, Macro: "m", Trigger: trig(1)},     // no way left to click
		{Family: "Mouse", Button: 0x100 + leftButton, Macro: "m", Trigger: trig(1)},                       // the left button once cut to a byte
		{Family: "Mouse", Button: 5, Layer: 0x100, Macro: "m", Trigger: trig(1)},                          // no such layer
		{Family: "Mouse", Button: 5, Macro: "m", Trigger: trig(0)},                                        // trigger taken
		{Family: "Mouse", Button: 4, Macro: "m", Trigger: trig(1)},                                        // button taken
		{Family: "Mouse", Button: 5, Macro: "gone", Trigger: trig(1)},                                     // no such macro
		{Family: "Mouse", Button: leftButton, Layer: razer.LayerHypershift, Macro: "m", Trigger: trig(2)}, // allowed, as in SetButton
		{Family: "Mouse", Button: 5, Macro: "m", Trigger: trig(3), Original: razer.Assignment{Fn: 0x99, Data: []byte{1, 2}}},
		{Family: "Mouse", Button: 3, Macro: "m", Trigger: trig(4), Original: triggerAssignment(trig(7))},
		{Family: kb.Name, Button: kb.Layout[key].Index, Macro: "m", Trigger: trig(5)},
		{Family: kb.Name, Button: kb.FnIndex, Macro: "m", Trigger: trig(6)}, // the Fn key cannot be remapped
		{Family: kb.Name, Button: noKey, Macro: "m", Trigger: trig(6)},      // not a key of this model
		{Family: viaFamilyPrefix + "1234:5678", Button: leftButton, Macro: "m", Trigger: trig(6)},
	}
	want := []Binding{
		in[0],
		in[7],
		{Family: "Mouse", Button: 5, Macro: "m", Trigger: trig(3), Original: razer.Assignment{Fn: 0x77, Data: []byte{9}}},
		{Family: "Mouse", Button: 3, Macro: "m", Trigger: trig(4), Original: defaultAssignment(3)},
		in[10],
		in[13],
	}
	want[1].Original = defaultAssignment(leftButton)    // it had none
	kept := razer.Assignment{Fn: 0x77, Data: []byte{9}} // what button 5 did here before it was bound
	got := importBindings(in, macros, []Binding{{Family: "Mouse", Button: 5, Original: kept}})
	if len(got) != len(want) {
		t.Fatalf("kept %d bindings, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Family != w.Family || g.Button != w.Button || g.Layer != w.Layer || g.Trigger != w.Trigger {
			t.Errorf("binding %d: %+v, want %+v", i, g, w)
		}
		if mouse := g.Family == "Mouse"; mouse && (g.Original.Fn != w.Original.Fn || !slices.Equal(g.Original.Data, w.Original.Data)) {
			t.Errorf("binding %d: original %+v, want %+v", i, g.Original, w.Original)
		}
	}
}

// A mouse that is asleep keeps sending its trigger keys. Replacing the
// settings then would forget what its buttons did, so the import waits.
func TestReplaceConfigWaitsForAnAsleepMouse(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	side := razer.Assignment{Fn: razer.FnMouse, Data: []byte{4}}
	bound := Binding{Family: "Razer Test", Button: 4, Macro: "m", Trigger: firstTrigger, Original: side}
	cfgMu.Lock()
	savedCfg := cfg
	cfg = defaultConfig()
	cfg.Macros, cfg.Bindings = []Macro{{ID: "m"}}, []Binding{bound}
	cfgMu.Unlock()
	devices.mu.Lock()
	savedDevs := devices.devs
	devices.devs = []*razer.Device{dev("BF", "Razer Test (Wireless)", false)}
	devices.mu.Unlock()
	origGet, origSet := buttonAssignment, setButtonAssignment
	t.Cleanup(func() {
		buttonAssignment, setButtonAssignment = origGet, origSet
		devices.mu.Lock()
		devices.devs = savedDevs
		devices.mu.Unlock()
		cfgMu.Lock()
		cfg = savedCfg
		cfgMu.Unlock()
	})
	var written []razer.Assignment
	setButtonAssignment = func(_ *razer.Device, _, _ int, a razer.Assignment) error {
		written = append(written, a)
		return nil
	}

	buttonAssignment = func(*razer.Device, int, int) (razer.Assignment, error) {
		return razer.Assignment{}, errors.New("no answer")
	}
	if err := replaceConfig(defaultConfig()); err == nil {
		t.Fatal("replaced the settings while a bound mouse was asleep")
	}
	cfgMu.Lock()
	kept := len(cfg.Bindings) == 1 && cfg.Bindings[0].Trigger == bound.Trigger
	cfgMu.Unlock()
	if !kept || len(written) != 0 {
		t.Fatalf("binding kept: %v, writes: %d; want the binding kept and no write", kept, len(written))
	}

	buttonAssignment = func(*razer.Device, int, int) (razer.Assignment, error) {
		return triggerAssignment(bound.Trigger), nil
	}
	if err := replaceConfig(defaultConfig()); err != nil {
		t.Fatal(err)
	}
	cfgMu.Lock()
	left := len(cfg.Bindings)
	cfgMu.Unlock()
	if left != 0 || len(written) != 1 || written[0].Fn != side.Fn || !slices.Equal(written[0].Data, side.Data) {
		t.Fatalf("%d bindings left, wrote %+v; want none left and the button's own function written", left, written)
	}
}

// The same for a VIA keyboard that does not answer: its macro key keeps the
// trigger, so the settings that say what the key did must stay.
func TestReplaceConfigWaitsForAViaKeyboard(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	gone := make(chan []byte)
	close(gone) // unplugged: every request ends at once
	d := via.NewDevice(&via.Model{Name: "Test 60", Rows: 5, Cols: 15, Keys: []via.Key{{Row: 0, Col: 0}}}, &fakeViaPort{in: gone})
	d.ID, d.Layers = "test:gone", 4
	cfgMu.Lock()
	savedCfg := cfg
	cfg = defaultConfig()
	cfg.Macros = []Macro{{ID: "m"}}
	cfg.Bindings = []Binding{{Family: viaFamily(d), Button: 0, Macro: "m", Trigger: firstTrigger}}
	cfg.ViaKeys = map[string]map[string]uint16{d.ID: {viaKeyOf(0, 0): 4}}
	cfgMu.Unlock()
	viaKeyboards.mu.Lock()
	viaKeyboards.devs = []*via.Device{d}
	viaKeyboards.mu.Unlock()
	t.Cleanup(func() {
		viaKeyboards.mu.Lock()
		viaKeyboards.devs = nil
		viaKeyboards.mu.Unlock()
		cfgMu.Lock()
		cfg = savedCfg
		cfgMu.Unlock()
	})

	if err := replaceConfig(defaultConfig()); err == nil {
		t.Fatal("replaced the settings while a VIA keyboard kept its macro key")
	}
	cfgMu.Lock()
	kept := len(cfg.Bindings) == 1 && len(cfg.ViaKeys[d.ID]) == 1
	cfgMu.Unlock()
	if !kept {
		t.Fatal("the binding or the key's remembered function was lost")
	}
}

// Quitting restores the buttons while a rescan may be replacing the devices.
// A device closed under the restore is looked up again, or its buttons would
// keep their trigger keys after the app is gone.
func TestRestoreBindingsFollowsARescan(t *testing.T) {
	side := razer.Assignment{Fn: razer.FnMouse, Data: []byte{4}}
	cfgMu.Lock()
	savedCfg := cfg
	cfg = defaultConfig()
	cfg.Bindings = []Binding{{Family: "Razer Test", Button: 4, Macro: "m", Trigger: firstTrigger, Original: side}}
	cfgMu.Unlock()
	old, fresh := dev("BF", "Razer Test (Wireless)", true), dev("BF", "Razer Test (Wireless)", true)
	devices.mu.Lock()
	savedDevs := devices.devs
	devices.devs = []*razer.Device{old}
	devices.mu.Unlock()
	origGet, origSet := buttonAssignment, setButtonAssignment
	t.Cleanup(func() {
		buttonAssignment, setButtonAssignment = origGet, origSet
		devices.mu.Lock()
		devices.devs = savedDevs
		devices.mu.Unlock()
		cfgMu.Lock()
		cfg = savedCfg
		cfgMu.Unlock()
	})
	buttonAssignment = func(d *razer.Device, _, _ int) (razer.Assignment, error) {
		if d == old { // the rescan lands between the list and the request
			devices.mu.Lock()
			devices.devs = []*razer.Device{fresh}
			devices.mu.Unlock()
			return razer.Assignment{}, razer.ErrClosed
		}
		return triggerAssignment(firstTrigger), nil
	}
	var restored []*razer.Device
	setButtonAssignment = func(d *razer.Device, _, _ int, a razer.Assignment) error {
		if a.Fn == side.Fn && slices.Equal(a.Data, side.Data) {
			restored = append(restored, d)
		}
		return nil
	}

	bindMu.Lock()
	missed := applyBindingsLocked(true)
	bindMu.Unlock()
	if len(restored) != 1 || restored[0] != fresh || len(missed) != 0 {
		t.Fatalf("restored %d buttons, missed %v; want the button restored on the rescanned device", len(restored), missed)
	}
}

func TestImportConfig(t *testing.T) {
	if _, err := importConfig([]byte(`{"foo":1}`)); err == nil {
		t.Fatal("accepted a file that is not a settings file")
	}
	if _, err := importConfig([]byte(`{"scenes":{"scenes":[{"name":"","apps":[]}]}}`)); err == nil {
		t.Fatal("accepted invalid scenes config")
	}
	if _, err := importConfig([]byte(`{"smartPoll":{"normalRate":12345}}`)); err == nil {
		t.Fatal("accepted invalid smart poll config")
	}
	for _, bad := range []string{
		`{"scenes":"oops"}`,
		`{"scenes":{"game":{"dpi":"oops"}}}`,
		`{"scenes":{},"smartPoll":{"normalRate":"oops"}}`,
		`{"scenes":{},"smart":"oops"}`,
		`{"scenes":{},"lighting":"oops"}`,
		`{"scenes":{},"notify":"oops"}`,
		`{"scenes":{},"dpiDisplay":"oops"}`,
		`{"scenes":{},"glass":"oops"}`,
		`{"scenes":{},"language":123}`,
		`{"scenes":{},"macros":"oops"}`,
		`{"scenes":{},"bindings":"oops"}`,
	} {
		if _, err := importConfig([]byte(bad)); err == nil {
			t.Fatalf("accepted invalid section: %s", bad)
		}
	}
	src := defaultConfig()
	src.Language = "ja"
	src.Macros = []Macro{{ID: "a", Name: "ok", Mode: "once"}, {ID: "b", Name: "", Mode: "once"}}
	src.Bindings = []Binding{{Family: "F", Button: 5, Macro: "a", Trigger: firstTrigger}, {Family: "F", Button: 4, Macro: "b", Trigger: firstTrigger + 1}}
	b, _ := json.Marshal(src)
	c, err := importConfig(b)
	if err != nil || c.Language != "ja" || len(c.Macros) != 1 || len(c.Bindings) != 1 || c.Bindings[0].Macro != "a" {
		t.Fatalf("%v %+v", err, c)
	}
	// Title rules come along, and untidy ones do not refuse the file.
	c, err = importConfig([]byte(`{"scenes":{"enabled":true,"scenes":[{"name":"Cloud","apps":[],"titles":[" GeForce NOW ","geforce now",""]}]}}`))
	if err != nil || len(c.Scenes.Scenes) != 1 || !slices.Equal(c.Scenes.Scenes[0].Titles, []string{"GeForce NOW"}) {
		t.Fatalf("%v %+v; want the title rule imported and tidied", err, c.Scenes)
	}
	if _, err := importConfig([]byte(`{"scenes":{"scenes":[{"name":"Cloud","titles":"oops"}]}}`)); err == nil {
		t.Fatal("accepted titles that are not a list")
	}
	c, err = importConfig([]byte(`{"smartPoll":{"normalRate":1000,"exceptions":[{"exe":"","title":" GeForce NOW ","rate":8000},{"exe":"osu!.exe","rate":4000}]}}`))
	if err != nil || !slices.Equal(c.Smart.Exceptions, []Rule{{Title: "GeForce NOW", Rate: 8000}, {Exe: "osu!.exe", Rate: 4000}}) {
		t.Fatalf("%v %+v; want the rate by title imported next to the one by program", err, c.Smart.Exceptions)
	}
}

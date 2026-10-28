package main

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

func TestSceneLightsRestore(t *testing.T) {
	sl := sceneLights{base: map[string]kbShow{}, on: map[string]kbShow{}}
	shown := kbShow{Lighting: leobog.Lighting{Effect: 12, Brightness: 4, Speed: 4, Colorful: true}}
	writes := 0
	fail := false
	read := func() (kbShow, error) { return shown, nil }
	write := func(st kbShow) error {
		if fail {
			return errors.New("busy")
		}
		writes++
		shown = st
		return nil
	}
	game := sceneShow(&Scene{Highlight: &SceneHighlight{Keys: []int{0, 9}, Color: razer.RGB{R: 255}}})
	if game.Lighting.Effect != leobog.EffectCustom || game.Keys[9] != (razer.RGB{R: 255}) || game.Keys[1] != (razer.RGB{}) {
		t.Fatalf("highlight: %+v", game.Lighting)
	}

	sl.step("k", game, read, write)
	sl.step("k", game, read, write) // already shown: no second write
	if shown != *game || writes != 1 {
		t.Fatalf("enter: %d writes", writes)
	}
	other := sceneShow(&Scene{Keyboard: &leobog.Lighting{Effect: 1, Brightness: 3}})
	sl.step("k", other, read, write) // scene to scene keeps the first baseline
	fail = true
	sl.step("k", nil, read, write) // a failed restore is tried again
	fail = false
	sl.step("k", nil, read, write)
	if shown.Lighting.Effect != 12 || !shown.Lighting.Colorful || len(sl.base) != 0 {
		t.Fatalf("leave: shown %+v, base %v", shown.Lighting, sl.base)
	}
	n := writes
	sl.step("k", nil, read, write) // nothing pending: nothing written
	if writes != n {
		t.Fatal("wrote without a baseline")
	}
}

func TestKeyActions(t *testing.T) {
	def := leobog.Models[0].DefaultLayers()
	caps := def[0][3]
	for _, act := range []ButtonAction{
		{Kind: "default"},
		{Kind: "key", Code: 0x41, Mods: 1}, // Ctrl+A
		{Kind: "key", Mods: 2},             // Shift alone
		{Kind: "media", Code: 0xe9},
	} {
		a, err := actionFor(act, caps)
		if err != nil {
			t.Fatalf("%+v: %v", act, err)
		}
		if got := keyAction(a, caps); got != act {
			t.Fatalf("%+v came back as %+v", act, got)
		}
	}
	for _, act := range []ButtonAction{{Kind: "key"}, {Kind: "key", Code: 0x7C}, {Kind: "media", Code: 1}, {Kind: "other"}} {
		if _, err := actionFor(act, caps); err == nil {
			t.Fatalf("%+v accepted", act)
		}
	}
	// Fn-layer functions the page cannot set show as other, and stay as they are.
	if got := keyAction(def[1][8], leobog.Action{}); got.Kind != "other" {
		t.Fatalf("Fn+Q: %+v", got)
	}
}

func TestKeymapWithMacros(t *testing.T) {
	cfgMu.Lock()
	saved := cfg
	cfg = defaultConfig()
	cfg.Bindings = []Binding{{Family: "LEOBOG Hi75", Button: 9, Layer: 1, Macro: "m", Trigger: firstTrigger + 2}}
	cfgMu.Unlock()
	defer func() { cfgMu.Lock(); cfg = saved; cfgMu.Unlock() }()
	d := &leobog.Device{Model: leobog.Models[0], ID: "k", Name: "LEOBOG Hi75"}
	if got := keymapOf(d, false)[1][9]; got != (leobog.Action{Type: leobog.ActionKey, Code: 0x6a}) {
		t.Fatalf("Fn+A sends %+v, not F15", got)
	}
	if keymapOf(d, true) != leobog.Models[0].DefaultLayers() {
		t.Fatal("restore kept the trigger")
	}
}

func TestKeyboardResetSuccessSavesBindings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)

	colors := leobog.DefaultColors()
	keyColors := leobog.Models[0].FactoryKeyColors()

	cfgMu.Lock()
	savedCfg := cfg
	cfg = defaultConfig()
	cfg.Keyboards = map[string]KeyboardMemory{
		"k": {Colors: colors, KeyColors: keyColors},
	}
	cfg.Bindings = []Binding{{Family: "LEOBOG Hi75", Button: 9, Layer: 1, Macro: "m", Trigger: firstTrigger + 2}}
	cfgMu.Unlock()
	t.Cleanup(func() {
		cfgMu.Lock()
		cfg = savedCfg
		cfgMu.Unlock()
	})

	if err := saveConfig(); err != nil {
		t.Fatal(err)
	}

	d := &leobog.Device{Model: leobog.Models[0], ID: "k", Name: "LEOBOG Hi75"}
	d.SetColors(colors)
	d.SetKeyColors(keyColors)

	keyboards.mu.Lock()
	savedDevs := keyboards.devs
	savedWritten := keyboards.written
	keyboards.devs = []*leobog.Device{d}
	keyboards.written = map[string]*[leobog.Layers]leobog.Layer{"k": nil}
	keyboards.mu.Unlock()
	t.Cleanup(func() {
		keyboards.mu.Lock()
		keyboards.devs = savedDevs
		keyboards.written = savedWritten
		keyboards.mu.Unlock()
	})

	origReset := deviceReset
	resetCalled := false
	deviceReset = func(dev *leobog.Device) error {
		if dev.ID != "k" {
			t.Fatalf("unexpected device %s", dev.ID)
		}
		resetCalled = true
		return nil
	}
	t.Cleanup(func() { deviceReset = origReset })

	if err := (Keyboards{}).Reset("k"); err != nil {
		t.Fatal(err)
	}
	if !resetCalled {
		t.Fatal("deviceReset was not called")
	}

	cfgMu.Lock()
	bindings := slices.Clone(cfg.Bindings)
	cfgMu.Unlock()
	if len(bindings) != 0 {
		t.Fatalf("bindings not deleted in memory: %+v", bindings)
	}

	keyboards.mu.Lock()
	gotWritten := keyboards.written["k"]
	keyboards.mu.Unlock()
	if gotWritten != nil {
		t.Fatalf("written not reset to nil: %v", gotWritten)
	}

	// Verify disk config has bindings deleted even when KeyboardMemory did not change.
	raw, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatal(err)
	}
	var onDisk config
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if len(onDisk.Bindings) != 0 {
		t.Fatalf("bindings not deleted on disk: %+v", onDisk.Bindings)
	}
}

func TestKeyboardResetFailureKeepsBindingsAndRewritesKeymap(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)

	cfgMu.Lock()
	savedCfg := cfg
	cfg = defaultConfig()
	cfg.Bindings = []Binding{{Family: "LEOBOG Hi75", Button: 9, Layer: 1, Macro: "m", Trigger: firstTrigger + 2}}
	cfgMu.Unlock()
	t.Cleanup(func() {
		cfgMu.Lock()
		cfg = savedCfg
		cfgMu.Unlock()
	})

	d := &leobog.Device{Model: leobog.Models[0], ID: "k", Name: "LEOBOG Hi75"}
	keyboards.mu.Lock()
	savedDevs := keyboards.devs
	savedWritten := keyboards.written
	keyboards.devs = []*leobog.Device{d}
	keyboards.written = map[string]*[leobog.Layers]leobog.Layer{"k": nil}
	keyboards.mu.Unlock()
	t.Cleanup(func() {
		keyboards.mu.Lock()
		keyboards.devs = savedDevs
		keyboards.written = savedWritten
		keyboards.mu.Unlock()
	})

	origReset := deviceReset
	origSetKeymap := deviceSetKeymap
	deviceReset = func(dev *leobog.Device) error {
		return errors.New("reset error")
	}
	var writtenKeymap *[leobog.Layers]leobog.Layer
	deviceSetKeymap = func(dev *leobog.Device, km [leobog.Layers]leobog.Layer) error {
		writtenKeymap = &km
		return nil
	}
	t.Cleanup(func() {
		deviceReset = origReset
		deviceSetKeymap = origSetKeymap
	})

	err := (Keyboards{}).Reset("k")
	if err == nil || err.Error() != "reset error" {
		t.Fatalf("expected reset error, got %v", err)
	}

	cfgMu.Lock()
	bindings := slices.Clone(cfg.Bindings)
	cfgMu.Unlock()
	if len(bindings) != 1 {
		t.Fatalf("bindings were deleted on failure: %+v", bindings)
	}

	keyboards.mu.Lock()
	gotWritten := keyboards.written["k"]
	keyboards.mu.Unlock()
	if gotWritten != &unknownKeymap {
		t.Fatalf("written keymap not marked unknown: %v", gotWritten)
	}

	// syncKeymaps should see the unknown keymap and rewrite the wanted keymap.
	bindMu.Lock()
	syncKeymaps(false)
	bindMu.Unlock()

	if writtenKeymap == nil {
		t.Fatal("syncKeymaps skipped rewriting the unknown keymap")
	}
	want := keymapOf(d, false)
	if *writtenKeymap != want {
		t.Fatal("syncKeymaps did not write the wanted keymap")
	}

	keyboards.mu.Lock()
	synced := keyboards.written["k"]
	keyboards.mu.Unlock()
	if synced == nil || synced == &unknownKeymap || *synced != want {
		t.Fatalf("written not updated after sync: %v", synced)
	}
}

func TestRescanKeepsUnknownKeymapForRewrite(t *testing.T) {
	d := &leobog.Device{Model: leobog.Models[0], ID: "k", Name: "LEOBOG Hi75"}
	knownMap := keymapOf(d, false)

	oldWritten := map[string]*[leobog.Layers]leobog.Layer{
		"k":  &unknownKeymap,
		"k2": &knownMap,
	}

	newWritten := nextWritten(oldWritten)
	if newWritten["k"] != &unknownKeymap {
		t.Fatalf("unknown keymap was not kept across rescan: %v", newWritten["k"])
	}
	if newWritten["k2"] != nil {
		t.Fatalf("known keymap was not dropped: %v", newWritten["k2"])
	}

	// Verify syncKeymaps rewrites the keymap after rescan keeps &unknownKeymap.
	keyboards.mu.Lock()
	savedDevs := keyboards.devs
	savedWritten := keyboards.written
	keyboards.devs = []*leobog.Device{d}
	keyboards.written = newWritten
	keyboards.mu.Unlock()
	t.Cleanup(func() {
		keyboards.mu.Lock()
		keyboards.devs = savedDevs
		keyboards.written = savedWritten
		keyboards.mu.Unlock()
	})

	origSetKeymap := deviceSetKeymap
	var writtenKeymap *[leobog.Layers]leobog.Layer
	deviceSetKeymap = func(dev *leobog.Device, km [leobog.Layers]leobog.Layer) error {
		writtenKeymap = &km
		return nil
	}
	t.Cleanup(func() { deviceSetKeymap = origSetKeymap })

	bindMu.Lock()
	syncKeymaps(false)
	bindMu.Unlock()

	if writtenKeymap == nil {
		t.Fatal("syncKeymaps skipped rewriting the keyboard kept as unknown across rescan")
	}
	want := keymapOf(d, false)
	if *writtenKeymap != want {
		t.Fatal("syncKeymaps wrote unexpected keymap")
	}
}

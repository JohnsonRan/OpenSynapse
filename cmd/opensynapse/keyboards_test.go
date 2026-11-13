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

// fakeKeyboard installs one Hi75 with the given written cache and records
// every keymap sent to it.
func fakeKeyboard(t testing.TB, written map[string]*[leobog.Layers]leobog.Layer) (*leobog.Device, *[][leobog.Layers]leobog.Layer) {
	t.Setenv("APPDATA", t.TempDir())
	cfgMu.Lock()
	savedCfg := cfg
	cfg = defaultConfig()
	cfgMu.Unlock()
	d := &leobog.Device{Model: leobog.Models[0], ID: "k", Name: "LEOBOG Hi75"}
	keyboards.mu.Lock()
	savedDevs, savedWritten := keyboards.devs, keyboards.written
	keyboards.devs, keyboards.written = []*leobog.Device{d}, written
	keyboards.mu.Unlock()
	origSetKeymap := deviceSetKeymap
	var sent [][leobog.Layers]leobog.Layer
	deviceSetKeymap = func(_ *leobog.Device, km [leobog.Layers]leobog.Layer) error {
		sent = append(sent, km)
		return nil
	}
	t.Cleanup(func() {
		deviceSetKeymap = origSetKeymap
		keyboards.mu.Lock()
		keyboards.devs, keyboards.written = savedDevs, savedWritten
		keyboards.mu.Unlock()
		cfgMu.Lock()
		cfg = savedCfg
		cfgMu.Unlock()
	})
	return d, &sent
}

// Nothing written yet: the remap is saved first, and must still reach the keyboard.
func TestSetKeyWritesBeforeAnySync(t *testing.T) {
	d, sent := fakeKeyboard(t, map[string]*[leobog.Layers]leobog.Layer{})
	if err := (Keyboards{}).SetKey("k", 0, 3, ButtonAction{Kind: "key", Code: 0x41, Mods: 1}); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 1 || (*sent)[0] != keymapOf(d, false) || (*sent)[0] == d.DefaultLayers() {
		t.Fatalf("keymaps sent: %d, want the remapped one", len(*sent))
	}
}

// The keyboard keeps its keymap across a reconnect, so after a rescan the
// trigger keys it holds are still known and quitting restores the keys.
func TestRescanKeepsWrittenKeymap(t *testing.T) {
	d, sent := fakeKeyboard(t, map[string]*[leobog.Layers]leobog.Layer{})
	cfgMu.Lock()
	cfg.Macros = []Macro{{ID: "m"}}
	cfgMu.Unlock()
	if err := (Keyboards{}).SetKey("k", 0, 3, ButtonAction{Kind: "macro", Macro: "m"}); err != nil {
		t.Fatal(err)
	}
	keyboards.mu.Lock()
	keyboards.written = nextWritten(keyboards.written)
	keyboards.mu.Unlock()
	bindMu.Lock()
	syncKeymaps(true)
	bindMu.Unlock()
	if len(*sent) != 2 || (*sent)[1] != keymapOf(d, true) {
		t.Fatalf("keymaps sent: %d, want the trigger map then the restored one", len(*sent))
	}
}

func TestRescanKeepsUnknownKeymapForRewrite(t *testing.T) {
	d := &leobog.Device{Model: leobog.Models[0], ID: "k", Name: "LEOBOG Hi75"}

	newWritten := nextWritten(map[string]*[leobog.Layers]leobog.Layer{"k": &unknownKeymap})
	if newWritten["k"] != &unknownKeymap {
		t.Fatalf("unknown keymap was not kept across rescan: %v", newWritten["k"])
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

// A keyboard that does not take the factory keymap keeps its macro bindings
// and remaps, and is marked for the rewrite that makes it match them again.
func TestResetKeysFailureKeepsBindingsAndRewritesKeymap(t *testing.T) {
	d, sent := fakeKeyboard(t, map[string]*[leobog.Layers]leobog.Layer{})
	cfgMu.Lock()
	cfg.Macros = []Macro{{ID: "m"}}
	cfgMu.Unlock()
	if err := (Keyboards{}).SetKey("k", 0, 3, ButtonAction{Kind: "key", Code: 0x41}); err != nil {
		t.Fatal(err)
	}
	if err := (Keyboards{}).SetKey("k", 0, 4, ButtonAction{Kind: "macro", Macro: "m"}); err != nil {
		t.Fatal(err)
	}
	want := keymapOf(d, false)
	record := deviceSetKeymap
	deviceSetKeymap = func(*leobog.Device, [leobog.Layers]leobog.Layer) error { return errors.New("busy") }
	if err := (Keyboards{}).ResetKeys("k"); err == nil || err.Error() != "busy" {
		t.Fatalf("ResetKeys: %v, want the write error", err)
	}
	deviceSetKeymap = record
	cfgMu.Lock()
	bound, remapped := len(cfg.Bindings), cfg.Keyboards["k"].Keymap != nil
	cfgMu.Unlock()
	if bound != 1 || !remapped {
		t.Fatalf("after a failed reset: %d bindings, remap kept %v; want both kept", bound, remapped)
	}
	*sent = nil
	bindMu.Lock()
	syncKeymaps(false)
	bindMu.Unlock()
	if len(*sent) != 1 || (*sent)[0] != want {
		t.Fatalf("keymaps sent after the failed reset: %d, want the bound keymap again", len(*sent))
	}
}

// A change the keyboard did not take is an error for the page, which goes on
// showing what the key did. The settings go back to that as well, and the
// next round writes the old keymap over what the failed write may have left.
func TestSetKeyFailedWriteRollsBack(t *testing.T) {
	d, sent := fakeKeyboard(t, map[string]*[leobog.Layers]leobog.Layer{})
	cfgMu.Lock()
	cfg.Macros = []Macro{{ID: "m"}}
	cfgMu.Unlock()
	if err := (Keyboards{}).SetKey("k", 0, 3, ButtonAction{Kind: "key", Code: 0x41}); err != nil {
		t.Fatal(err)
	}
	if err := (Keyboards{}).SetKey("k", 0, 4, ButtonAction{Kind: "macro", Macro: "m"}); err != nil {
		t.Fatal(err)
	}
	held, remapped := keymapOf(d, false), keymapOf(d, true)
	record := deviceSetKeymap
	deviceSetKeymap = func(*leobog.Device, [leobog.Layers]leobog.Layer) error { return errors.New("busy") }
	for name, change := range map[string]struct {
		index int
		act   ButtonAction
	}{
		"a remap":              {5, ButtonAction{Kind: "key", Code: 0x42}},
		"a remap undone":       {3, ButtonAction{Kind: "default"}},
		"a new macro key":      {9, ButtonAction{Kind: "macro", Macro: "m"}},
		"a macro key remapped": {4, ButtonAction{Kind: "key", Code: 0x43}},
	} {
		err := (Keyboards{}).SetKey("k", 0, change.index, change.act)
		if err == nil || err.Error() != tr("err.keyWrite") {
			t.Fatalf("%s: %v, want the message for a failed write", name, err)
		}
		cfgMu.Lock()
		bindings := slices.Clone(cfg.Bindings)
		cfgMu.Unlock()
		if keymapOf(d, true) != remapped || len(bindings) != 1 || bindings[0].Button != 4 || keymapOf(d, false) != held {
			t.Fatalf("%s: the keys and bindings changed though the keyboard refused: %+v", name, bindings)
		}
	}
	saved, _, err := func() (config, []string, error) {
		b, err := os.ReadFile(configPath())
		if err != nil {
			return config{}, nil, err
		}
		return parseConfig(b, defaultConfig())
	}()
	if err != nil || len(saved.Bindings) != 1 || saved.Keyboards["k"].Keymap == nil || *saved.Keyboards["k"].Keymap != remapped {
		t.Fatalf("saved settings after the refused changes: %+v, %v; want them as before", saved.Bindings, err)
	}
	deviceSetKeymap = record
	*sent = nil
	bindMu.Lock()
	syncKeymaps(false)
	bindMu.Unlock()
	if len(*sent) != 1 || (*sent)[0] != held {
		t.Fatalf("keymaps sent after the refused changes: %d, want the old keymap once", len(*sent))
	}
}

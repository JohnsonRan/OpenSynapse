package main

import (
	"errors"
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

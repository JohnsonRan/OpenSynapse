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

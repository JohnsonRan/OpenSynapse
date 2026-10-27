package main

import (
	"slices"
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

func TestButtonActions(t *testing.T) {
	cases := []struct {
		act  ButtonAction
		want razer.Assignment
	}{
		{ButtonAction{Kind: "default"}, razer.Assignment{Fn: razer.FnMouse, Data: []byte{5}}},
		{ButtonAction{Kind: "mouse", Code: 4}, razer.Assignment{Fn: razer.FnMouse, Data: []byte{4}}},
		{ButtonAction{Kind: "dpi", Code: 6}, razer.Assignment{Fn: razer.FnDPI, Data: []byte{6}}},
		{ButtonAction{Kind: "key", Code: 0x41, Mods: 1}, razer.Assignment{Fn: razer.FnKey, Data: []byte{1, 0x04}}}, // Ctrl+A
		{ButtonAction{Kind: "key", Code: 0x7C}, razer.Assignment{Fn: razer.FnKey, Data: []byte{0, 0x68}}},          // F13
		{ButtonAction{Kind: "key", Code: 0x10}, razer.Assignment{Fn: razer.FnKey, Data: []byte{2, 0}}},             // Shift alone
	}
	for _, c := range cases {
		a, err := assignmentOf(5, razer.LayerNormal, c.act)
		if err != nil || a.Fn != c.want.Fn || !slices.Equal(a.Data, c.want.Data) {
			t.Errorf("%+v -> %+v %v", c.act, a, err)
			continue
		}
		back := actionOf(5, a)
		if c.act.Code == 0x10 { // a modifier key reads back as its bit
			c.act = ButtonAction{Kind: "key", Mods: 2}
		}
		if back != c.act {
			t.Errorf("%+v read back as %+v", c.act, back)
		}
	}
	for _, bad := range []ButtonAction{{Kind: "mouse", Code: 7}, {Kind: "dpi", Code: 3}, {Kind: "key", Code: 0xFF}, {Kind: "wheel"}} {
		if _, err := assignmentOf(5, razer.LayerNormal, bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if a, err := assignmentOf(4, razer.LayerNormal, ButtonAction{Kind: "hypershift"}); err != nil || a.Fn != razer.FnHypershift || !slices.Equal(a.Data, []byte{1}) {
		t.Errorf("hypershift: %+v %v", a, err)
	}
	if _, err := assignmentOf(4, razer.LayerHypershift, ButtonAction{Kind: "hypershift"}); err == nil {
		t.Error("Hypershift on its own layer")
	}
	if actionOf(0x60, razer.Assignment{Fn: razer.FnDPI, Data: []byte{6}}).Kind != "default" {
		t.Error("the DPI button's own function is its default")
	}
}

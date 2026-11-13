package main

import (
	"slices"
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

func TestButtonActions(t *testing.T) {
	v4 := mouseModel("Razer DeathAdder V4 Pro")
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
		a, err := assignmentOf(v4, 5, razer.LayerNormal, c.act)
		if err != nil || a.Fn != c.want.Fn || !slices.Equal(a.Data, c.want.Data) {
			t.Errorf("%+v -> %+v %v", c.act, a, err)
			continue
		}
		back := actionOf(v4, 5, a)
		if c.act.Code == 0x10 { // a modifier key reads back as its bit
			c.act = ButtonAction{Kind: "key", Mods: 2}
		}
		if back != c.act {
			t.Errorf("%+v read back as %+v", c.act, back)
		}
	}
	for _, bad := range []ButtonAction{{Kind: "mouse", Code: 7}, {Kind: "dpi", Code: 3}, {Kind: "key", Code: 0xFF}, {Kind: "wheel"}} {
		if _, err := assignmentOf(v4, 5, razer.LayerNormal, bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if a, err := assignmentOf(v4, 4, razer.LayerNormal, ButtonAction{Kind: "hypershift"}); err != nil || a.Fn != razer.FnHypershift || !slices.Equal(a.Data, []byte{1}) {
		t.Errorf("hypershift: %+v %v", a, err)
	}
	if _, err := assignmentOf(v4, 4, razer.LayerHypershift, ButtonAction{Kind: "hypershift"}); err == nil {
		t.Error("Hypershift on its own layer")
	}
	if actionOf(v4, 0x60, razer.Assignment{Fn: razer.FnDPI, Data: []byte{6}}).Kind != "default" {
		t.Error("the DPI button's own function is its default")
	}
}

// The sensitivity clutch: one DPI for both axes, on the DeathAdder V4 Pro
// alone. The bytes are checked against Synapse's encoding in internal/razer.
func TestClutchAction(t *testing.T) {
	v4 := mouseModel("Razer DeathAdder V4 Pro")
	other := &razer.Model{Name: "Razer Viper V3 Pro (Wired)", Category: "mouse", MaxDPI: 35000}
	if v4 == nil || mouseModel("No Such Mouse") != nil {
		t.Fatal("mouseModel finds a catalog family, and nothing for an unknown one")
	}
	clutch := razer.Assignment{Fn: razer.FnDPI, Data: []byte{5, 0x01, 0x90, 0x01, 0x90}}
	for _, layer := range []int{razer.LayerNormal, razer.LayerHypershift} {
		a, err := assignmentOf(v4, 4, layer, ButtonAction{Kind: "clutch", DPI: 400})
		if err != nil || a.Fn != clutch.Fn || !slices.Equal(a.Data, clutch.Data) {
			t.Errorf("layer %d: %+v %v, want %+v", layer, a, err, clutch)
		}
	}
	if back := actionOf(v4, 4, clutch); back != (ButtonAction{Kind: "clutch", DPI: 400}) {
		t.Errorf("a clutch at 400 read back as %+v", back)
	}
	for _, dpi := range []int{0, 99, 45001} {
		if a, err := assignmentOf(v4, 4, razer.LayerNormal, ButtonAction{Kind: "clutch", DPI: dpi}); err == nil {
			t.Errorf("%d DPI accepted: %+v", dpi, a)
		}
	}
	for name, m := range map[string]*razer.Model{"another mouse": other, "an unknown mouse": nil} {
		if a, err := assignmentOf(m, 4, razer.LayerNormal, ButtonAction{Kind: "clutch", DPI: 400}); err == nil {
			t.Errorf("%s: clutch accepted: %+v", name, a)
		}
		if back := actionOf(m, 4, clutch); back.Kind != "other" {
			t.Errorf("%s: a clutch read back as %+v, want other", name, back)
		}
	}
	// X and Y apart: the page has one value for both, so it cannot show this one.
	if back := actionOf(v4, 4, razer.Assignment{Fn: razer.FnDPI, Data: []byte{5, 0x01, 0x90, 0x03, 0x20}}); back.Kind != "other" {
		t.Errorf("a clutch with X and Y apart read back as %+v, want other", back)
	}

	// SetButton refuses the clutch for another mouse before anything is sent.
	// Neither device has a HID collection: the DeathAdder V4 Pro's write gets
	// past the check and fails for that reason instead.
	devices.mu.Lock()
	saved := devices.devs
	devices.devs = []*razer.Device{{Model: other, ID: "other"}, {Model: v4, ID: "v4"}}
	devices.mu.Unlock()
	t.Cleanup(func() {
		devices.mu.Lock()
		devices.devs = saved
		devices.mu.Unlock()
	})
	act := ButtonAction{Kind: "clutch", DPI: 400}
	if err := (Devices{}).SetButton("other", 4, razer.LayerNormal, act); err == nil || err.Error() != tr("err.buttonAction") {
		t.Errorf("clutch on another mouse: %v, want %q", err, tr("err.buttonAction"))
	}
	if err := (Devices{}).SetButton("v4", 4, razer.LayerNormal, act); err == nil || err.Error() == tr("err.buttonAction") {
		t.Errorf("clutch on the DeathAdder V4 Pro: %v, want it to pass the check", err)
	}

	// A clutch is a function the app can name, so a binding's Original that
	// is one survives an import; on a family without it, the default returns.
	in := []Binding{
		{Family: "Razer DeathAdder V4 Pro", Button: 4, Macro: "m", Trigger: firstTrigger, Original: clutch},
		{Family: "Razer Viper V3 Pro", Button: 4, Macro: "m", Trigger: firstTrigger + 1, Original: clutch},
	}
	got := importBindings(in, []Macro{{ID: "m"}}, nil)
	if len(got) != 2 || !slices.Equal(got[0].Original.Data, clutch.Data) || got[0].Original.Fn != razer.FnDPI {
		t.Errorf("import on the DeathAdder V4 Pro: %+v, want the clutch kept", got)
	}
	if def := defaultAssignment(4); len(got) != 2 || got[1].Original.Fn != def.Fn || !slices.Equal(got[1].Original.Data, def.Data) {
		t.Errorf("import on another mouse: %+v, want the button's default", got)
	}
}

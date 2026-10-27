package main

import (
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

func dev(id, name string, answered bool) *razer.Device {
	return &razer.Device{Model: &razer.Model{Name: name}, ID: id, Answered: answered}
}

func ids(devs []*razer.Device) (out []string) {
	for _, d := range devs {
		out = append(out, d.ID)
	}
	return out
}

func TestConnectedPairsWiredAndWireless(t *testing.T) {
	kb := dev("kb", "Razer BlackWidow V3", true)
	cases := []struct {
		name string
		devs []*razer.Device
		want string
	}{
		// cable plugged: the receiver stays attached but its mouse answers on the cable
		{"cable", []*razer.Device{dev("BF", "Razer DeathAdder V4 Pro (Wireless)", false), dev("BE", "Razer DeathAdder V4 Pro (Wired)", true), kb}, "BE"},
		// both answer (receiver relays too): prefer the cable
		{"both", []*razer.Device{dev("BF", "Razer DeathAdder V4 Pro (Wireless)", true), dev("BE", "Razer DeathAdder V4 Pro (Wired)", true), kb}, "BE"},
		// wired ID enumerated but silent (charging only): wireless wins
		{"charging", []*razer.Device{dev("BF", "Razer DeathAdder V4 Pro (Wireless)", true), dev("BE", "Razer DeathAdder V4 Pro (Wired)", false), kb}, "BF"},
		// nothing answers (mouse asleep): keep the wireless ID
		{"asleep", []*razer.Device{dev("BE", "Razer DeathAdder V4 Pro (Wired)", false), dev("BF", "Razer DeathAdder V4 Pro (Wireless)", false), kb}, "BF"},
	}
	for _, c := range cases {
		got := ids(connected(c.devs))
		if len(got) != 2 || got[1] != "kb" || got[0] != c.want {
			t.Errorf("%s: got %v, want [%s kb]", c.name, got, c.want)
		}
	}
	if got := ids(connected([]*razer.Device{dev("BF", "Razer DeathAdder V4 Pro (Wireless)", false)})); len(got) != 1 {
		t.Errorf("a lone silent device must stay listed: %v", got)
	}
}

func TestTrayLabel(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{trayLabel("Razer DeathAdder V4 Pro", true, false, 95, true), "DeathAdder V4 Pro · " + tr("tray.wired") + " · 95% ⚡"},
		{trayLabel("Razer DeathAdder V4 Pro", false, true, 40, false), "DeathAdder V4 Pro · " + tr("tray.wireless") + " · 40%"},
		{trayLabel("Razer BlackWidow V3", false, false, -1, false), "BlackWidow V3"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

package main

import "testing"

func TestSwitcherDebounce(t *testing.T) {
	var writes []int
	ok := true
	sw := &switcher{apply: func(hz int) bool { writes = append(writes, hz); return ok }}
	for _, hz := range []int{8000, 1000, 8000, 8000, 8000, 1000, 1000} {
		sw.observe(hz)
	}
	if len(writes) != 2 || writes[0] != 8000 || writes[1] != 1000 {
		t.Fatalf("writes = %v, want [8000 1000]", writes)
	}
	ok = false // device asleep: keep retrying until it takes
	sw.observe(4000)
	sw.observe(4000)
	sw.observe(4000)
	if sw.applied != 1000 || len(writes) != 4 {
		t.Fatalf("applied %d writes %v", sw.applied, writes)
	}
}

func TestDecide(t *testing.T) {
	c := SmartConfig{DefaultRate: 1000, AutoDetect: true, AutoRate: 4000,
		Rules: []Rule{{"cs2.exe", 8000}, {"vivaldi.exe", 2000}, {"game.exe", 500}}}
	cases := []struct {
		exe        string
		fullscreen bool
		gpu        float64
		known      bool
		rate       int
		why        string
	}{
		{"cs2.exe", false, -1, false, 8000, reasonRule},     // rule wins even windowed
		{"game.exe", true, 90, true, 500, reasonRule},       // rule beats detection
		{"elden.exe", true, 60, false, 4000, reasonGame},    // fullscreen + GPU
		{"elden.exe", false, -1, true, 4000, reasonGame},    // remembered game, windowed menu
		{"elden.exe", true, 3, false, 1000, reasonDefault},  // fullscreen video-like load
		{"elden.exe", true, -1, false, 1000, reasonDefault}, // not measured yet
		{"code.exe", false, 80, false, 1000, reasonDefault}, // windowed GPU load is not a game
		{"chrome.exe", true, 80, false, 1000, reasonDefault},
		{"vivaldi.exe", true, 80, false, 2000, reasonRule},
	}
	for _, k := range cases {
		if rate, why := decide(c, k.exe, k.fullscreen, k.gpu, k.known); rate != k.rate || why != k.why {
			t.Errorf("%+v: got %d %s", k, rate, why)
		}
	}
	c.AutoDetect = false
	if rate, _ := decide(c, "elden.exe", true, 90, true); rate != 1000 {
		t.Error("auto-detect off still detected")
	}
}

func TestNormalizeRules(t *testing.T) {
	c := SmartConfig{DefaultRate: 1000, AutoRate: 4000, Rules: []Rule{{` C:\Games\CS2.EXE `, 8000}, {"", 500}}}
	if err := c.normalize(); err != nil || len(c.Rules) != 1 || c.Rules[0].Exe != "cs2.exe" {
		t.Fatalf("%v %+v", err, c.Rules)
	}
	c.AutoRate = 333
	if c.normalize() == nil {
		t.Fatal("accepted 333 Hz")
	}
	c.AutoRate = 4000
	c.Rules[0].Rate = 333
	if c.normalize() == nil {
		t.Fatal("accepted 333 Hz")
	}
}

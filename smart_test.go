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

func TestNormalizeRules(t *testing.T) {
	c := SmartConfig{DefaultRate: 1000, Rules: []Rule{{` C:\Games\CS2.EXE `, 8000}, {"", 500}}}
	if err := c.normalize(); err != nil || len(c.Rules) != 1 || c.Rules[0].Exe != "cs2.exe" {
		t.Fatalf("%v %+v", err, c.Rules)
	}
	if targetRate(c, "cs2.exe") != 8000 || targetRate(c, "chrome.exe") != 1000 {
		t.Fatal("targetRate")
	}
	c.Rules[0].Rate = 333
	if c.normalize() == nil {
		t.Fatal("accepted 333 Hz")
	}
}

package main

import (
	"errors"
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
)

func TestSceneLightsRestore(t *testing.T) {
	sl := sceneLights{base: map[string]leobog.Lighting{}, on: map[string]leobog.Lighting{}}
	shown := leobog.Lighting{Effect: 12, Brightness: 4, Speed: 4, Colorful: true}
	writes := 0
	fail := false
	read := func() (leobog.Lighting, error) { return shown, nil }
	write := func(l leobog.Lighting) error {
		if fail {
			return errors.New("busy")
		}
		writes++
		shown = l
		return nil
	}
	game := leobog.Lighting{Effect: 2, Brightness: 2, Speed: 1}

	sl.step("k", &game, read, write)
	sl.step("k", &game, read, write) // already shown: no second write
	if shown != game || writes != 1 {
		t.Fatalf("enter: shown %+v after %d writes", shown, writes)
	}
	other := leobog.Lighting{Effect: 1, Brightness: 3}
	sl.step("k", &other, read, write) // scene to scene keeps the first baseline
	fail = true
	sl.step("k", nil, read, write) // a failed restore is tried again
	fail = false
	sl.step("k", nil, read, write)
	if shown.Effect != 12 || !shown.Colorful || len(sl.base) != 0 {
		t.Fatalf("leave: shown %+v, base %v", shown, sl.base)
	}
	n := writes
	sl.step("k", nil, read, write) // nothing pending: nothing written
	if writes != n {
		t.Fatal("wrote without a baseline")
	}
}

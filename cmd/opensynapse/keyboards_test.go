package main

import (
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

func TestSceneLighting(t *testing.T) {
	red := razer.RGB{R: 0xff}
	cur := leobog.Lighting{Effect: 12, Brightness: 4, Speed: 4, Colorful: true}
	staticStored := leobog.Lighting{Effect: 1, Brightness: 2, Speed: 0, Colorful: true}
	pct := func(v int) *int { return &v }

	if _, ok := sceneLighting(Scene{}, cur, cur); ok {
		t.Fatal("a scene without lighting changed the keyboard")
	}
	// The effect comes with what the keyboard keeps for it, and the scene's color.
	got, _ := sceneLighting(Scene{Lighting: &Lighting{Effect: razer.EffectStatic, Color: red}}, cur, staticStored)
	if got != (leobog.Lighting{Effect: 1, Brightness: 2, Color: red}) {
		t.Fatalf("static: %+v", got)
	}
	// Brightness alone keeps the effect; a dim percentage stays lit.
	got, _ = sceneLighting(Scene{Brightness: pct(10)}, cur, cur)
	if got.Effect != 12 || got.Brightness != 1 {
		t.Fatalf("brightness: %+v", got)
	}
	if got, _ = sceneLighting(Scene{Brightness: pct(0)}, cur, cur); got.Brightness != 0 {
		t.Fatalf("0%%: %+v", got)
	}
	got, _ = sceneLighting(Scene{Lighting: &Lighting{Effect: razer.EffectWave}}, cur, leobog.Lighting{Effect: 13, Brightness: 3, Color: red})
	if got.Effect != 13 || !got.Colorful || got.Check() != nil {
		t.Fatalf("wave: %+v", got)
	}
	got, _ = sceneLighting(Scene{Lighting: &Lighting{Effect: razer.EffectOff}, Brightness: pct(100)}, cur, leobog.Lighting{})
	if got != (leobog.Lighting{}) || got.Check() != nil {
		t.Fatalf("off: %+v", got)
	}
	for e, id := range sceneEffects {
		if _, ok := leobog.Lookup(id); !ok {
			t.Fatalf("%s maps to unknown effect %d", e, id)
		}
	}
}

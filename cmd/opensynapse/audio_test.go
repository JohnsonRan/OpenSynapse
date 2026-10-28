package main

import (
	"encoding/json"
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/walkplay"
)

func flatEQ() walkplay.EQ {
	e := walkplay.EQ{}
	for _, f := range []int{31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000} {
		e.Bands = append(e.Bands, walkplay.Band{Freq: f, Q: 0.75, Type: "peak"})
	}
	return e
}

func TestEQPresets(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	saved := cfg
	defer func() { cfg = saved }()
	cfg = defaultConfig()

	a := Audio{}
	e := flatEQ()
	if a.SavePreset(EQPreset{Name: " ", EQ: e}) == nil {
		t.Fatal("saved a preset without a name")
	}
	if a.SavePreset(EQPreset{Name: "x", EQ: walkplay.EQ{}}) == nil {
		t.Fatal("saved an EQ without bands")
	}
	if err := a.SavePreset(EQPreset{Name: " Music ", EQ: e}); err != nil {
		t.Fatal(err)
	}
	e.Preamp = -3
	if err := a.SavePreset(EQPreset{Name: "Music", EQ: e}); err != nil {
		t.Fatal(err)
	}
	if p := a.Presets(); len(p) != 1 || p[0].Name != "Music" || p[0].EQ.Preamp != -3 {
		t.Fatalf("same name must replace: %+v", p)
	}
	if err := a.DeletePreset("Music"); err != nil || len(a.Presets()) != 0 {
		t.Fatalf("delete: %v %+v", err, a.Presets())
	}
}

func TestImportDropsInvalidPresets(t *testing.T) {
	c, err := importConfig([]byte(`{"scenes":{},"eqPresets":[{"name":"bad","eq":{"bands":[]}},{"name":"ok","eq":` + mustJSON(t, flatEQ()) + `}]}`))
	if err != nil || len(c.EQPresets) != 1 || c.EQPresets[0].Name != "ok" {
		t.Fatalf("%v %+v", err, c.EQPresets)
	}
	if _, err := importConfig([]byte(`{"scenes":{},"eqPresets":"oops"}`)); err == nil {
		t.Fatal("accepted a malformed preset list")
	}
}

func mustJSON(t *testing.T, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

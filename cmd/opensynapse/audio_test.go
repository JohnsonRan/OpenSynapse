package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/walkplay"
	"github.com/JohnsonRan/OpenSynapse/internal/winaudio"
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

func TestSceneDeviceRestoresTheDefault(t *testing.T) {
	cur, sets := "speakers", 0
	var failOn string
	soundDefault = func(winaudio.Flow) (string, error) { return cur, nil }
	setSoundDefault = func(id string) error {
		if failOn == id {
			return errors.New("device not ready")
		}
		cur = id
		sets++
		return nil
	}
	defer func() { soundDefault, setSoundDefault = winaudio.Default, winaudio.SetDefault }()

	var sd sceneDevice
	sd.apply(winaudio.Playback, "")
	if sets != 0 {
		t.Fatal("changed the default with no scene")
	}

	// 1. Injected failure on activate
	failOn = "headset"
	sd.apply(winaudio.Playback, "headset")
	if cur != "speakers" || sd.on != "" || sd.base != "speakers" {
		t.Fatalf("failed activate must not set on: cur=%s, on=%s, base=%s", cur, sd.on, sd.base)
	}
	failOn = ""
	sd.apply(winaudio.Playback, "headset") // retry succeeds
	if cur != "headset" || sd.on != "headset" || sets != 1 {
		t.Fatalf("retry activate failed: cur=%s, on=%s, sets=%d", cur, sd.on, sets)
	}

	sd.apply(winaudio.Playback, "headset") // every tick: no new switch
	if cur != "headset" || sets != 1 {
		t.Fatalf("cur %s after %d switches", cur, sets)
	}
	sd.apply(winaudio.Playback, "monitor") // another scene: the default from before stays remembered

	// 2. Injected failure on restore
	failOn = "speakers"
	sd.apply(winaudio.Playback, "")
	if cur != "monitor" || sd.base != "speakers" {
		t.Fatalf("failed restore must keep base: cur=%s, base=%s", cur, sd.base)
	}
	failOn = ""
	sd.apply(winaudio.Playback, "") // retry succeeds
	if cur != "speakers" || sets != 3 || sd.base != "" || sd.on != "" {
		t.Fatalf("restored to %s after %d switches (base=%s, on=%s)", cur, sets, sd.base, sd.on)
	}
	sd.apply(winaudio.Playback, "")
	if sets != 3 {
		t.Fatal("restored twice")
	}

	// 3. A switch that fails after changing the default must not count as the previous one.
	sd.apply(winaudio.Playback, "headset")
	failOn = "monitor"
	setFail := setSoundDefault
	setSoundDefault = func(id string) error { cur = id; return setFail(id) } // changes, then fails
	sd.apply(winaudio.Playback, "monitor")
	setSoundDefault = setFail
	failOn = ""
	sd.apply(winaudio.Playback, "headset")
	if cur != "headset" {
		t.Fatalf("back to the headset scene left %s", cur)
	}
}

func TestReconcileSceneAudio(t *testing.T) {
	sceneAudio.Lock()
	sceneAudio.eqBase["d1"] = flatEQ()
	sceneAudio.eqOn["d1"] = "on1"
	sceneAudio.eqBase["d2"] = flatEQ()
	sceneAudio.eqOn["d2"] = "on2"
	sceneAudio.Unlock()
	defer func() {
		sceneAudio.Lock()
		clear(sceneAudio.eqBase)
		clear(sceneAudio.eqOn)
		sceneAudio.Unlock()
	}()

	reconcileSceneAudio([]string{"d1"})

	sceneAudio.Lock()
	defer sceneAudio.Unlock()
	if _, ok := sceneAudio.eqBase["d1"]; !ok {
		t.Fatal("d1 base was removed")
	}
	if _, ok := sceneAudio.eqOn["d1"]; !ok {
		t.Fatal("d1 on was removed")
	}
	if _, ok := sceneAudio.eqBase["d2"]; ok {
		t.Fatal("d2 base was not removed")
	}
	if _, ok := sceneAudio.eqOn["d2"]; ok {
		t.Fatal("d2 on was not removed")
	}
}

func TestSavePresetDoesNotMutateSnapshot(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	saved := cfg
	defer func() { cfg = saved }()
	cfg = defaultConfig()

	e := flatEQ()
	a := Audio{}
	if err := a.SavePreset(EQPreset{Name: "GameEQ", EQ: e}); err != nil {
		t.Fatal(err)
	}
	cfg.Scenes.Game.EQ = &SceneEQ{Preset: "u:GameEQ", EQ: e}

	cfgMu.Lock()
	snapshot := cfg.Scenes
	cfgMu.Unlock()

	e2 := flatEQ()
	e2.Preamp = -6
	if err := a.SavePreset(EQPreset{Name: "GameEQ", EQ: e2}); err != nil {
		t.Fatal(err)
	}

	if snapshot.Game.EQ.EQ.Preamp != 0 {
		t.Fatalf("snapshot was mutated: preamp=%d, want 0", snapshot.Game.EQ.EQ.Preamp)
	}
	cfgMu.Lock()
	current := cfg.Scenes.Game.EQ.EQ.Preamp
	cfgMu.Unlock()
	if current != -6 {
		t.Fatalf("current was not updated: preamp=%d, want -6", current)
	}
}

func TestSameEQ(t *testing.T) {
	a, b := flatEQ(), flatEQ()
	b.Bands[2].Q = 0.7 + 1.0/512 // stored to 1/256
	if !sameEQ(a, a) || sameEQ(a, walkplay.EQ{}) {
		t.Fatal("equal EQs differ, or different ones match")
	}
	a.Bands[2].Q = 0.7
	if !sameEQ(a, b) {
		t.Fatal("Q within rounding counted as different")
	}
	b.Preamp = -1
	if sameEQ(a, b) {
		t.Fatal("preamp ignored")
	}
}

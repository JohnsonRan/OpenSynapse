package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigDeadlockOnInvalidScene(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	cfgDir := filepath.Join(dir, "OpenSynapse")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// An invalid scene (empty name) triggers normalize error which calls tr() -> language() -> cfgMu.Lock()
	badCfg := []byte(`{"scenes":{"scenes":[{"name":"","apps":[]}]}}`)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), badCfg, 0o644); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		loadConfig()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loadConfig deadlocked on invalid scene")
	}
}

func TestLoadConfigCorruptedJSONBackedUp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	cfgDir := filepath.Join(dir, "OpenSynapse")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(cfgDir, "config.json")
	if err := os.WriteFile(p, []byte(`{not valid json`), 0o644); err != nil {
		t.Fatal(err)
	}

	loadConfig()

	badPath := p + ".bad"
	if _, err := os.Stat(badPath); err != nil {
		t.Fatalf("corrupted config was not renamed to .bad: %v", err)
	}
}

func TestLoadConfigCorruptedRenameFailedBlocksSave(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	cfgDir := filepath.Join(dir, "OpenSynapse")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(cfgDir, "config.json")
	corrupted := []byte(`{not valid json`)
	if err := os.WriteFile(p, corrupted, 0o644); err != nil {
		t.Fatal(err)
	}
	// Create config.json.bad as a non-empty directory so rename fails.
	badDir := p + ".bad"
	if err := os.MkdirAll(filepath.Join(badDir, "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		saveMu.Lock()
		saveBlocked = false
		saveMu.Unlock()
	})

	loadConfig()

	if err := saveConfig(); err == nil {
		t.Fatal("expected saveConfig to fail when backup failed")
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupted) {
		t.Fatalf("expected corrupted config to be preserved, got: %s", string(got))
	}
}

// writeConfig puts a settings file into a temporary APPDATA and returns its
// path; the settings in memory and the save block come back after the test.
func writeConfig(t *testing.T, content string) string {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	p := filepath.Join(dir, "OpenSynapse", "config.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgMu.Lock()
	saved := cfg
	cfg = defaultConfig()
	cfgMu.Unlock()
	t.Cleanup(func() {
		cfgMu.Lock()
		cfg = saved
		cfgMu.Unlock()
		saveMu.Lock()
		saveBlocked = false
		saveMu.Unlock()
	})
	return p
}

// One key that does not decode: the others load, and the file is kept as it
// was before a save writes the default over that key.
func TestLoadConfigUnreadKeyBackedUp(t *testing.T) {
	const content = `{"notify":false,"macros":"oops","keyboards":7}`
	p := writeConfig(t, content)

	loadConfig()

	cfgMu.Lock()
	notify := cfg.Notify
	cfgMu.Unlock()
	if notify {
		t.Fatal("the readable keys were not loaded")
	}
	if got, err := os.ReadFile(p + ".bad"); err != nil || string(got) != content {
		t.Fatalf("backup: %q, %v; want the file as it was", got, err)
	}
	if err := saveConfig(); err != nil {
		t.Fatalf("saving after the backup: %v", err)
	}
}

// loadScenes loads a settings file with these custom scenes, and checks that
// the file was kept as it was before anything could be saved over it.
func loadScenes(t *testing.T, scenes string) ScenesConfig {
	content := `{"notify":false,"scenes":{"enabled":true,"scenes":[` + scenes + `]}}`
	p := writeConfig(t, content)
	loadConfig()
	cfgMu.Lock()
	sc, notify := cfg.Scenes, cfg.Notify
	cfgMu.Unlock()
	if notify || !sc.Enabled {
		t.Fatalf("notify %v, scenes enabled %v; want the other settings loaded", notify, sc.Enabled)
	}
	if got, err := os.ReadFile(p + ".bad"); err != nil || string(got) != content {
		t.Fatalf("backup: %q, %v; want the file as it was", got, err)
	}
	return sc
}

// A scene without a name, with nothing set, as a settings file from before
// 2026-10-30 holds it: loading keeps the scene and names it.
func TestLoadConfigNamesAnUnnamedScene(t *testing.T) {
	const unnamed = `{"name":"","apps":[],"dpi":0,"lighting":null,"brightness":null,"eq":null,
		"output":"","input":"","volume":null,"keyboard":null,"highlight":null}`
	sc := loadScenes(t, unnamed)
	if len(sc.Scenes) != 1 || sc.Scenes[0].Name != tr("scene.unnamed", 1) {
		t.Fatalf("scenes %+v; want the scene kept and named", sc.Scenes)
	}
	if err := sc.normalize(); err != nil {
		t.Fatalf("the loaded scenes cannot be saved from the page: %v", err)
	}

	// The name is the scene's place, or the next number no scene has.
	sc = loadScenes(t, `{"name":" ","apps":["a.exe"]},{"name":"`+tr("scene.unnamed", 1)+`"},{"name":""}`)
	if len(sc.Scenes) != 3 || sc.Scenes[0].Name != tr("scene.unnamed", 2) || sc.Scenes[2].Name != tr("scene.unnamed", 3) {
		t.Fatalf("scenes %+v; want names no other scene has", sc.Scenes)
	}
	if len(sc.Scenes[0].Apps) != 1 {
		t.Fatalf("the named scene lost its programs: %+v", sc.Scenes[0])
	}
}

// A setting that is not valid goes back to "leave alone"; the scene and its
// other settings stay. A highlight key past the keyboard's keys panicked the
// scene switcher on every start, so none may be left.
func TestLoadConfigRepairsSceneSettings(t *testing.T) {
	cases := map[string]struct {
		scene    string
		repaired func(Scene) bool
	}{
		"dpi":        {`"dpi":99999`, func(s Scene) bool { return s.DPI == 0 }},
		"lighting":   {`"lighting":{"effect":"disco"}`, func(s Scene) bool { return s.Lighting == nil }},
		"brightness": {`"brightness":150`, func(s Scene) bool { return s.Brightness == nil }},
		"volume":     {`"volume":-3`, func(s Scene) bool { return s.Volume == nil }},
		"eq":         {`"eq":{"preset":"u:x","eq":{"bands":[],"preamp":0}}`, func(s Scene) bool { return s.EQ == nil }},
		"keyboard":   {`"keyboard":{"effect":-7}`, func(s Scene) bool { return s.Keyboard == nil }},
		"some highlight keys": {`"highlight":{"keys":[4,126,-1,200]}`, func(s Scene) bool {
			return s.Highlight != nil && len(s.Highlight.Keys) == 1 && s.Highlight.Keys[0] == 4
		}},
		"all highlight keys": {`"highlight":{"keys":[126]}`, func(s Scene) bool { return s.Highlight == nil }},
	}
	for name, c := range cases {
		sc := loadScenes(t, `{"name":"work","apps":["a.exe"],"output":"speakers",`+c.scene+`}`)
		if len(sc.Scenes) != 1 {
			t.Fatalf("%s: the scene was dropped", name)
		}
		s := sc.Scenes[0]
		if s.Name != "work" || len(s.Apps) != 1 || s.Output != "speakers" || !c.repaired(s) {
			t.Errorf("%s: loaded %+v; want that setting reset and the rest kept", name, s)
		}
		if err := s.normalize(); err != nil {
			t.Errorf("%s: still not valid: %v", name, err)
		}
		sceneShow(&s) // panics on a highlight key out of range
	}
	// The Game scene, which has no name to lose, is repaired the same way.
	p := writeConfig(t, `{"scenes":{"game":{"apps":["cs2.exe"],"dpi":1600,"highlight":{"keys":[126]}}}}`)
	loadConfig()
	cfgMu.Lock()
	game := cfg.Scenes.Game
	cfgMu.Unlock()
	if game.Highlight != nil || game.DPI != 1600 || len(game.Apps) != 1 {
		t.Fatalf("game scene %+v; want only its highlight reset", game)
	}
	if _, err := os.Stat(p + ".bad"); err != nil {
		t.Fatalf("no backup of the repaired file: %v", err)
	}
}

func TestLoadConfigUnreadKeyBackupFailedBlocksSave(t *testing.T) {
	const content = `{"notify":false,"macros":"oops"}`
	p := writeConfig(t, content)
	if err := os.MkdirAll(filepath.Join(p+".bad", "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}

	loadConfig()

	if err := saveConfig(); err == nil {
		t.Fatal("saved over a file with an unread key that has no backup")
	}
	if got, err := os.ReadFile(p); err != nil || string(got) != content {
		t.Fatalf("settings file: %q, %v; want it untouched", got, err)
	}
}

// A settings file as 0.9.1 wrote it has no title rules: it loads as it is,
// with nothing reported and no backup made.
func TestLoadConfigOlderFileHasNoTitles(t *testing.T) {
	const scene = `"dpi":0,"lighting":null,"brightness":null,"eq":null,"output":"","input":"","volume":null,"keyboard":null,"highlight":null}`
	p := writeConfig(t, `{"smartPoll":{"enabled":true,"gameRate":0,"normalRate":1000,"exceptions":[{"exe":"osu!.exe","rate":4000}]},
		"scenes":{"enabled":true,"game":{"name":"game","apps":["windowed.exe"],`+scene+`,"desktop":{"name":"desktop","apps":null,`+scene+`,
		"scenes":[{"name":"Work","apps":["code.exe"],`+scene+`]},
		"lighting":null,"notify":false,"language":"","glass":false,"dpiDisplay":true,"macros":[],"bindings":[],"eqPresets":[]}`)

	loadConfig()

	cfgMu.Lock()
	sc, sp := cfg.Scenes, cfg.Smart
	cfgMu.Unlock()
	if !sc.Enabled || len(sc.Scenes) != 1 || sc.Scenes[0].Name != "Work" || len(sc.Scenes[0].Apps) != 1 || len(sc.Game.Apps) != 1 {
		t.Fatalf("scenes %+v; want them as the file has them", sc)
	}
	if sc.Scenes[0].Titles != nil || sc.Game.Titles != nil || wantTitle(sc) {
		t.Fatalf("scenes %+v; want no title rule", sc)
	}
	if len(sp.Exceptions) != 1 || sp.Exceptions[0].Exe != "osu!.exe" {
		t.Fatalf("smart polling %+v; want its exception kept", sp)
	}
	if _, err := os.Stat(p + ".bad"); !os.IsNotExist(err) {
		t.Fatalf("a backup of a valid older file: %v", err)
	}
}

// Title rules survive a save and a load; a setup without them is written
// without the key, as 0.9.1 wrote it. The desktop scene has no rules: titles
// a hand-edited file gives it are not kept.
func TestLoadConfigKeepsTitles(t *testing.T) {
	p := writeConfig(t, `{}`)
	c := ScenesConfig{Enabled: true, Scenes: []Scene{{Name: "Cloud", Titles: []string{"GeForce NOW"}}, {Name: "Work", Apps: []string{"code.exe"}}}}
	if err := (Scenes{}).SetConfig(c); err != nil {
		t.Fatal(err)
	}
	drainAutoReset()
	saved, err := os.ReadFile(p)
	if err != nil || strings.Count(string(saved), `"titles"`) != 1 {
		t.Fatalf("saved file, %v: %s\nwant the titles key on the one scene that has a rule", err, saved)
	}
	cfgMu.Lock()
	cfg = defaultConfig()
	cfgMu.Unlock()
	loadConfig()
	cfgMu.Lock()
	sc := cfg.Scenes
	cfgMu.Unlock()
	if len(sc.Scenes) != 2 || !slices.Equal(sc.Scenes[0].Titles, []string{"GeForce NOW"}) || sc.Scenes[1].Titles != nil {
		t.Fatalf("loaded %+v; want the title rule back", sc.Scenes)
	}

	p = writeConfig(t, `{"scenes":{"enabled":true,"desktop":{"titles":["x"]},"game":{"titles":[" Y ","y"]}}}`)
	loadConfig()
	cfgMu.Lock()
	sc = cfg.Scenes
	cfgMu.Unlock()
	if sc.Desktop.Titles != nil || !slices.Equal(sc.Game.Titles, []string{"Y"}) {
		t.Fatalf("desktop titles %q, game titles %q; want none on the desktop scene and the game scene's tidied", sc.Desktop.Titles, sc.Game.Titles)
	}
	if _, err := os.Stat(p + ".bad"); !os.IsNotExist(err) {
		t.Fatalf("tidying titles counted as a repair: %v", err)
	}
}

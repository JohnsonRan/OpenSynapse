package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"
)

// The window title is read only for a title rule of a feature that is on.
func TestWantTitle(t *testing.T) {
	titled := []Scene{{Name: "a", Apps: []string{"a.exe"}}, {Name: "b", Titles: []string{"x"}}}
	byProgram := SmartPoll{Enabled: true, Exceptions: []Rule{{Exe: "a.exe", Rate: 500}}}
	byTitle := SmartPoll{Enabled: true, Exceptions: []Rule{{Exe: "a.exe", Rate: 500}, {Title: "x", Rate: 500}}}
	cases := map[string]struct {
		sc   ScenesConfig
		sp   SmartPoll
		lock int
		want bool
	}{
		"no rules":                {ScenesConfig{Enabled: true, Scenes: titled[:1]}, byProgram, noScene, false},
		"a custom scene's title":  {ScenesConfig{Enabled: true, Scenes: titled}, byProgram, noScene, true},
		"the game scene's title":  {ScenesConfig{Enabled: true, Game: Scene{Titles: []string{"x"}}}, SmartPoll{}, noScene, true},
		"scenes off":              {ScenesConfig{Scenes: titled, Game: Scene{Titles: []string{"x"}}}, byProgram, noScene, false},
		"a rate by title":         {ScenesConfig{Enabled: true, Scenes: titled[:1]}, byTitle, noScene, true},
		"smart polling off":       {ScenesConfig{}, SmartPoll{Exceptions: byTitle.Exceptions}, noScene, false},
		"a locked scene":          {ScenesConfig{Enabled: true, Scenes: titled}, byProgram, desktop, false},
		"locked, a rate by title": {ScenesConfig{Enabled: true, Scenes: titled}, byTitle, 0, true},
	}
	for name, k := range cases {
		if got := wantTitle(k.sc, k.sp, k.lock); got != k.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}

// foregroundIs makes the rounds of this test see f in front, and returns
// where each round's request for the title is recorded.
func foregroundIs(t *testing.T, f foreground) *[]bool {
	orig := getForeground
	asked := &[]bool{}
	getForeground = func(wantTitle bool) foreground {
		*asked = append(*asked, wantTitle)
		got := f
		if !wantTitle {
			got.title = ""
		}
		return got
	}
	t.Cleanup(func() { getForeground = orig })
	return asked
}

// A window title is compared with the rules and kept nowhere. Planted
// violations this turns red on: st.Foreground = f.title and
// log.Println(f.title) in auto.round.
func TestAutoStatusCarriesNoTitle(t *testing.T) {
	offConfig(t)
	const marker = "Quarterly-Secret-7f3a"
	asked := foregroundIs(t, foreground{pid: 4242, exe: "excel.exe", title: marker + " - Excel"})
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		cfgMu.Lock()
		cfg.Scenes.Enabled = false
		cfgMu.Unlock()
		newAuto().round() // the tray and the status forget the scene
	})
	cfgMu.Lock()
	// The scene sets nothing, so the round writes to no sound device.
	cfg.Scenes = ScenesConfig{Enabled: true, Game: Scene{Name: "game"}, Desktop: Scene{Name: "desktop"},
		Scenes: []Scene{{Name: "Books", Titles: []string{"secret-7F3A"}}}}
	cfgMu.Unlock()

	a := newAuto()
	a.round()
	a.round()
	st := Scenes{}.Status()
	if st.Scene != "Books" || st.SceneWhy != reasonRule || len(*asked) != 2 || !(*asked)[0] {
		t.Fatalf("status %+v, title asked for %v; want the scene matched by the title", st, *asked)
	}
	if err := saveConfig(); err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatal(err)
	}
	status, _ := json.Marshal(st)
	trayMu.Lock()
	tray := trayAuto
	trayMu.Unlock()
	for where, text := range map[string]string{"status": string(status), "tray": tray, "log": logged.String(), "settings file": string(file)} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(marker)) {
			t.Errorf("the window title is in the %s: %s", where, text)
		}
	}
	if !strings.Contains(tray, "Books") || !strings.Contains(string(file), "secret-7F3A") {
		t.Fatalf("tray %q, settings file %s; want the scene and its rule there, or the check above saw nothing", tray, file)
	}
}

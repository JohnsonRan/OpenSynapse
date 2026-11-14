package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

// lockedConfig turns scenes on with two custom scenes and no lock, on a
// temporary APPDATA.
func lockedConfig(t *testing.T) ScenesConfig {
	offConfig(t)
	t.Cleanup(func() {
		cfgMu.Lock()
		sceneLock, cfg.Scenes.Enabled = noScene, false
		cfgMu.Unlock()
		newAuto().round() // the tray and the status forget the scene
		drainAutoReset()
	})
	c := ScenesConfig{Enabled: true, Scenes: []Scene{{Name: "Work", Apps: []string{"code.exe"}}, {Name: "R&D", Apps: []string{"lab.exe"}}}}
	if err := (Scenes{}).SetConfig(c); err != nil {
		t.Fatal(err)
	}
	drainAutoReset()
	return Scenes{}.Config()
}

func lockIs() int {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return sceneLock
}

// choose clicks the item of the lock submenu with this label.
func choose(t *testing.T, menu *mygo.MenuItem, label string) {
	for _, it := range menu.Submenu {
		if it.Label == label {
			it.Click(it, nil)
			return
		}
	}
	t.Fatalf("no %q in the lock menu", label)
}

// What ends a lock, and what a click on a tray menu built earlier may lock.
func TestSceneLock(t *testing.T) {
	c := lockedConfig(t)
	set := func(c ScenesConfig) {
		t.Helper()
		// A copy, as a call from the page brings one: c is edited again below.
		if err := (Scenes{}).SetConfig(c.clone()); err != nil {
			t.Fatal(err)
		}
	}

	// The menu: automatic first and checked, then the scenes in matching order.
	menu := lockMenu()
	var labels, checked []string
	for _, it := range menu.Submenu {
		labels = append(labels, it.Label)
		if it.Checked {
			checked = append(checked, it.Label)
		}
		if it.Type != mygo.MenuItemRadio {
			t.Fatalf("%q is not a radio item: it would split the group", it.Label)
		}
	}
	want := []string{tr("tray.lockAuto"), "Work", "R&&D", tr("scene.game"), tr("scene.desktop")}
	if strings.Join(labels, "|") != strings.Join(want, "|") || strings.Join(checked, "|") != tr("tray.lockAuto") || menu.Disabled {
		t.Fatalf("lock menu %q, checked %q, disabled %v; want %q with automatic checked", labels, checked, menu.Disabled, want)
	}

	// Locking wakes the switcher, and the round uses the scene whatever is in front.
	foregroundIs(t, foreground{pid: 7, exe: "code.exe"})
	choose(t, menu, "R&&D")
	if lockIs() != 1 || !drainAutoReset() {
		t.Fatalf("lock %d; want the second scene locked and the switcher woken", lockIs())
	}
	a := newAuto()
	a.round()
	a.round()
	trayMu.Lock()
	tray := trayAuto
	trayMu.Unlock()
	if st := (Scenes{}).Status(); st.Scene != "R&D" || st.SceneWhy != reasonLock || tray != tr("tray.sceneLocked", "R&D") {
		t.Fatalf("status %+v, tray %q; want the locked scene over the program's", st, tray)
	}
	if it := lockMenu().Submenu[2]; !it.Checked || lockMenu().Submenu[0].Checked {
		t.Fatal("the menu does not show the locked scene")
	}

	// The lock is for the session: the settings file does not hold it.
	if err := saveConfig(); err != nil {
		t.Fatal(err)
	}
	var saved map[string]json.RawMessage
	file, _ := os.ReadFile(configPath())
	if err := json.Unmarshal(file, &saved); err != nil || len(saved) == 0 || strings.Contains(strings.ToLower(string(file)), "lock") {
		t.Fatalf("settings file, %v: %s\nwant no lock in it", err, file)
	}

	// Editing a scene keeps the lock, and so does the page's save of the same list.
	c.Scenes[1].DPI, c.Scenes[0].Name = 1600, "Office"
	set(c)
	if lockIs() != 1 {
		t.Fatal("editing the scenes released the lock")
	}
	// Renaming the locked scene releases it.
	c.Scenes[1].Name = "Lab"
	set(c)
	if lockIs() != noScene {
		t.Fatal("the lock survived renaming its scene")
	}

	// Deleting the locked scene and adding one within one save of the page:
	// the list is as long as before, with another scene at the place.
	lockScene(0, "Office")
	c.Scenes = []Scene{c.Scenes[1], {Name: "New"}}
	set(c)
	if lockIs() != noScene {
		t.Fatal("the lock moved to the scene that took the deleted one's place")
	}
	// A shorter list releases a custom scene's lock, not the game scene's.
	lockScene(1, "New")
	drainAutoReset()
	c.Scenes = c.Scenes[:1]
	set(c)
	if lockIs() != noScene {
		t.Fatal("the lock survived its scene")
	}
	lockScene(gameScene, "")
	c.Scenes = nil
	set(c)
	if lockIs() != gameScene {
		t.Fatal("deleting custom scenes released the game scene's lock")
	}

	// A click on a menu built before the list changed locks nothing.
	c.Scenes = []Scene{{Name: "Work"}, {Name: "Play"}}
	set(c)
	Scenes{}.Unlock()
	if lockIs() != noScene || !drainAutoReset() {
		t.Fatal("unlocking did not release the lock and wake the switcher")
	}
	stale := lockMenu()
	c.Scenes = c.Scenes[1:]
	set(c)
	drainAutoReset()
	choose(t, stale, "Play") // index 1 is past the list now
	choose(t, stale, "Work") // index 0 is "Play" now
	if lockIs() != noScene || drainAutoReset() {
		t.Fatalf("a stale tray click locked scene %d", lockIs())
	}
	Scenes{}.Unlock()
	if drainAutoReset() {
		t.Fatal("unlocking with nothing locked reset the switchers")
	}

	// Turning scenes off releases the lock, and nothing locks while they are off.
	choose(t, lockMenu(), tr("scene.desktop"))
	if lockIs() != desktop {
		t.Fatal("the desktop scene did not lock")
	}
	c.Enabled = false
	set(c)
	if lockIs() != noScene {
		t.Fatal("the lock survived turning scenes off")
	}
	off := lockMenu()
	choose(t, off, tr("scene.game"))
	if lockIs() != noScene || !off.Disabled {
		t.Fatalf("lock %d, menu disabled %v; want no lock with scenes off", lockIs(), off.Disabled)
	}

	// Importing settings replaces the scenes: the lock goes.
	c.Enabled = true
	set(c)
	lockScene(0, "Play")
	next := defaultConfig()
	next.Scenes = Scenes{}.Config()
	if err := replaceConfig(next); err != nil {
		t.Fatal(err)
	}
	if lockIs() != noScene {
		t.Fatal("the lock survived an import")
	}
}

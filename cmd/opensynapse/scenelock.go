package main

import (
	"strings"

	"github.com/egoist/mygo"
)

// sceneLock is the scene the tray locked: it stays on whatever is in front.
// noScene is no lock. It needs cfgMu, so a round reads it together with the
// scenes it indexes. It is not saved: scenes have no ID that a saved lock
// could name, and a restart goes back to automatic switching.
var sceneLock = noScene

// lockScene locks a scene from the tray menu. name is the custom scene's
// name when the menu was built: the list may have changed since, and the
// index then names another scene or none.
func lockScene(idx int, name string) {
	cfgMu.Lock()
	sc := cfg.Scenes
	ok := sc.Enabled && (idx == gameScene || idx == desktop || idx >= 0 && idx < len(sc.Scenes) && sc.Scenes[idx].Name == name)
	if ok {
		sceneLock = idx
	}
	cfgMu.Unlock()
	if ok {
		resetAuto() // the next round applies the scene, and the polling rate again
	}
}

// Unlock goes back to switching scenes automatically.
func (Scenes) Unlock() {
	cfgMu.Lock()
	locked := sceneLock != noScene
	sceneLock = noScene
	cfgMu.Unlock()
	if locked {
		resetAuto()
	}
}

// releaseLockFor drops the lock that next, about to replace the scenes, no
// longer fits: scenes are off, or the locked custom scene is gone. Without
// an ID a custom scene is the same one only at the same place, in a list of
// the same length, under the same name; so renaming it unlocks it too. It
// needs cfgMu.
func releaseLockFor(next ScenesConfig) {
	old := cfg.Scenes.Scenes
	if !next.Enabled || sceneLock >= 0 && (len(next.Scenes) != len(old) || next.Scenes[sceneLock].Name != old[sceneLock].Name) {
		sceneLock = noScene
	}
}

// lockMenu is the tray's submenu that locks a scene, in the order scenes are
// matched. The tray menu is built anew for each right-click, so the items
// carry the state of that moment.
func lockMenu() *mygo.MenuItem {
	cfgMu.Lock()
	sc, lock := cfg.Scenes.clone(), sceneLock
	cfgMu.Unlock()
	item := func(label string, idx int, name string) *mygo.MenuItem {
		return &mygo.MenuItem{Label: label, Type: mygo.MenuItemRadio, Checked: lock == idx,
			Click: func(*mygo.MenuItem, *mygo.Window) { lockScene(idx, name) }}
	}
	// No separator: it would split the radio group in two.
	items := []*mygo.MenuItem{{Label: tr("tray.lockAuto"), Type: mygo.MenuItemRadio, Checked: lock == noScene,
		Click: func(*mygo.MenuItem, *mygo.Window) { Scenes{}.Unlock() }}}
	for i, s := range sc.Scenes {
		items = append(items, item(strings.ReplaceAll(s.Name, "&", "&&"), i, s.Name)) // a single & marks an access key
	}
	items = append(items, item(tr("scene.game"), gameScene, ""), item(tr("scene.desktop"), desktop, ""))
	return &mygo.MenuItem{Label: tr("tray.lock"), Disabled: !sc.Enabled, Submenu: items}
}

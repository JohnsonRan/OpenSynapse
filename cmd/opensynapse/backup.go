package main

import (
	"encoding/json"
	"errors"
	"os"
	"slices"

	"github.com/egoist/mygo"
)

// Settings can be saved to a file and loaded on another PC: scenes, smart
// polling, macros and button bindings, lighting and the app's options.

var backupFilter = []mygo.FileFilter{{Name: "OpenSynapse", Extensions: []string{"json"}}}

// Export saves the settings to a file the user picks. It reports false when
// the user cancels.
func (Settings) Export() (bool, error) {
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent: currentWindow(), DefaultPath: "OpenSynapse-settings.json", Filters: backupFilter,
	})
	if err != nil || path == "" {
		return false, err
	}
	cfgMu.Lock()
	b, err := json.MarshalIndent(cfg, "", "  ")
	cfgMu.Unlock()
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, b, 0o644)
}

// Import replaces the settings with a file the user picks. It reports false
// when the user cancels. Buttons bound to macros are restored first, and the
// imported bindings take over once their devices are found.
func (Settings) Import() (bool, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: currentWindow(), Filters: backupFilter})
	if err != nil || len(paths) == 0 {
		return false, err
	}
	b, err := os.ReadFile(paths[0])
	if err != nil {
		return false, err
	}
	next, err := importConfig(b)
	if err != nil {
		return false, err
	}
	bindMu.Lock()
	applyBindingsLocked(true)
	cfgMu.Lock()
	next.KeyboardColors = cfg.KeyboardColors // what the keyboards here show, not the other PC's
	cfg = next
	cfgMu.Unlock()
	bindMu.Unlock()
	if err := saveConfig(); err != nil {
		return false, err
	}
	resetAuto()
	kickBindings()
	refreshHooks()
	if w := currentWindow(); w != nil && glassSupported {
		w.SetVibrancy(glassVibrancy(glassOn()))
	}
	return true, nil
}

// importConfig reads a settings file; one that is not OpenSynapse's fails.
func importConfig(b []byte) (config, error) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil || raw["scenes"] == nil && raw["macros"] == nil && raw["smartPoll"] == nil {
		return config{}, errors.New(tr("err.importFile"))
	}
	check := func(key string, target any) error {
		if v, ok := raw[key]; ok {
			return json.Unmarshal(v, target)
		}
		return nil
	}
	if check("scenes", &ScenesConfig{}) != nil ||
		check("smartPoll", &SmartPoll{}) != nil ||
		check("smart", &legacySmart{}) != nil ||
		check("lighting", &map[string]Lighting{}) != nil ||
		check("notify", new(bool)) != nil ||
		check("dpiDisplay", new(bool)) != nil ||
		check("glass", new(bool)) != nil ||
		check("language", new(string)) != nil ||
		check("macros", &[]Macro{}) != nil ||
		check("bindings", &[]Binding{}) != nil ||
		check("eqPresets", &[]EQPreset{}) != nil {
		return config{}, errors.New(tr("err.importFile"))
	}
	c, err := parseConfig(b, defaultConfig())
	if err != nil {
		return config{}, errors.New(tr("err.importFile"))
	}
	if err := c.Scenes.normalize(); err != nil {
		return config{}, errors.New(tr("err.importFile"))
	}
	if err := c.Smart.normalize(); err != nil {
		return config{}, errors.New(tr("err.importFile"))
	}
	c.Macros = slices.DeleteFunc(c.Macros, func(m Macro) bool { return m.normalize() != nil })
	c.EQPresets = slices.DeleteFunc(c.EQPresets, func(p EQPreset) bool { return p.normalize() != nil })
	c.Bindings = slices.DeleteFunc(c.Bindings, func(bd Binding) bool {
		return !isTriggerVK(bd.Trigger) || !slices.ContainsFunc(c.Macros, func(m Macro) bool { return m.ID == bd.Macro })
	})
	return c, nil
}

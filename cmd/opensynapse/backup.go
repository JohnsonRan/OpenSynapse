package main

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/egoist/mygo"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
	"github.com/JohnsonRan/OpenSynapse/internal/razer"
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
	if err := replaceConfig(next); err != nil {
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

// replaceConfig makes imported settings the current ones and saves them. The
// bound buttons get their own function back first. A mouse that is asleep
// cannot take it, and the import would forget what its buttons did while they
// still send their trigger keys, so the import is refused until it answers.
func replaceConfig(next config) error {
	bindMu.Lock()
	defer bindMu.Unlock() // MyGo recovers a panic in a call from the page: the lock must not stay held
	if missed := applyBindingsLocked(true); len(missed) > 0 {
		kickBindings() // the buttons that were restored play their macros again
		return errors.New(tr("err.importAsleep", strings.Join(missed, ", ")))
	}
	cfgMu.Lock()
	next.Keyboards = cfg.Keyboards // what the keyboards here hold, not the other PC's
	next.ViaKeys, next.ViaModels = cfg.ViaKeys, cfg.ViaModels
	cfg = next
	sceneLock = noScene // the scenes are other scenes now
	cfgMu.Unlock()
	return saveConfig()
}

// importBindings keeps the bindings the app could have made itself. The
// rest would reach a device as they are: a macro on the left button leaves no
// way to click, two bindings with one trigger key play the wrong macro, and
// Original is written to the mouse when the app quits. A mouse button and
// layer are sent as one byte each, so a larger number would land on another
// button. current are the bindings in use: a button bound here already keeps
// the function it had here, when the file's cannot be used.
func importBindings(in []Binding, macros []Macro, current []Binding) []Binding {
	out := []Binding{}
	for _, b := range in {
		taken := slices.ContainsFunc(out, func(o Binding) bool {
			return o.Trigger == b.Trigger || o.Family == b.Family && o.Button == b.Button && o.Layer == b.Layer
		})
		if taken || !isTriggerVK(b.Trigger) || !slices.ContainsFunc(macros, func(m Macro) bool { return m.ID == b.Macro }) {
			continue
		}
		if b.Layer != razer.LayerNormal && b.Layer != razer.LayerHypershift {
			continue
		}
		if kb := keyboardFamily(b.Family); kb != nil {
			if !slices.ContainsFunc(kb.Layout, func(k leobog.Key) bool { return k.Index == b.Button }) || b.Button == kb.FnIndex {
				continue
			}
		} else if !strings.HasPrefix(b.Family, viaFamilyPrefix) { // a mouse
			if b.Button < 0 || b.Button > 0xFF || b.Button == leftButton && b.Layer == razer.LayerNormal {
				continue
			}
			// An original the app cannot name, or a trigger key, is not written to
			// a mouse from a file: the button gets what it did here before it was
			// bound, or else its own function.
			m := mouseModel(b.Family)
			if _, err := assignmentOf(m, b.Button, b.Layer, actionOf(m, b.Button, b.Original)); err != nil || isTriggerAssignment(b.Original) {
				b.Original = defaultAssignment(b.Button)
				if i := slices.IndexFunc(current, func(c Binding) bool {
					return c.Family == b.Family && c.Button == b.Button && c.Layer == b.Layer
				}); i >= 0 {
					b.Original = current[i].Original
				}
			}
		}
		out = append(out, b)
	}
	return out
}

// keyboardFamily is the LEOBOG model a binding's family names, nil for none.
func keyboardFamily(family string) *leobog.Model {
	if i := slices.IndexFunc(leobog.Models, func(m *leobog.Model) bool { return m.Name == family }); i >= 0 {
		return leobog.Models[i]
	}
	return nil
}

// importConfig reads a settings file; one that is not OpenSynapse's fails.
func importConfig(b []byte) (config, error) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil || raw["scenes"] == nil && raw["macros"] == nil && raw["smartPoll"] == nil {
		return config{}, errors.New(tr("err.importFile"))
	}
	c, unread, err := parseConfig(b, defaultConfig())
	if err != nil || len(unread) > 0 {
		return config{}, errors.New(tr("err.importFile"))
	}
	if err := c.Smart.normalize(); err != nil {
		return config{}, errors.New(tr("err.importFile"))
	}
	// A second macro with an ID already taken is dropped like a bad one. The
	// bindings, the page and Save find a macro by its ID, so a duplicate could
	// take the first one's place without being flagged.
	ids := map[string]bool{}
	c.Macros = slices.DeleteFunc(c.Macros, func(m Macro) bool {
		if m.ID == "" || ids[m.ID] || m.normalize() != nil {
			return true
		}
		ids[m.ID] = true
		return false
	})
	for i := range c.Macros {
		// Whatever the file says: a program a backup names starts only after
		// the user has seen it on this PC.
		c.Macros[i].Imported = c.Macros[i].launches()
	}
	c.EQPresets = slices.DeleteFunc(c.EQPresets, func(p EQPreset) bool { return p.normalize() != nil })
	cfgMu.Lock()
	current := slices.Clone(cfg.Bindings)
	cfgMu.Unlock()
	c.Bindings = importBindings(c.Bindings, c.Macros, current)
	return c, nil
}

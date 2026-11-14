package main

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"slices"
	"strings"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
)

// Loading the settings file: reading it, the forms older versions wrote,
// and the repair of what is not valid in it. The settings themselves and
// their validation are in config.go.

// legacySmart is the config of the smart polling-rate page that scenes replaced.
type legacySmart struct {
	Enabled     bool `json:"enabled"`
	DefaultRate int  `json:"defaultRate"`
	Rules       []struct {
		Exe  string `json:"exe"`
		Rate int    `json:"rate"`
	} `json:"rules"`
	AutoDetect bool `json:"autoDetect"`
	AutoRate   int  `json:"autoRate"`
}

// smartPoll converts the first smart polling-rate config.
func (l legacySmart) smartPoll() SmartPoll {
	if l.AutoRate == 0 {
		l.AutoRate = 8000 // the first version had no auto-detection fields
	}
	c := SmartPoll{Enabled: l.Enabled, GameRate: l.AutoRate, NormalRate: l.DefaultRate, Exceptions: []Rule{}}
	for _, r := range l.Rules {
		c.Exceptions = append(c.Exceptions, Rule{Exe: r.Exe, Rate: r.Rate})
	}
	return c
}

func loadConfig() {
	p := configPath()
	b, err := os.ReadFile(p)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("cannot read config %s: %v; saving disabled", p, err)
			saveMu.Lock()
			saveBlocked = true
			saveMu.Unlock()
			return
		}
		saveMu.Lock()
		saveBlocked = false
		saveMu.Unlock()
		return // first run: defaults
	}
	cfgMu.Lock()
	def := cfg
	cfgMu.Unlock()
	next, unread, err := parseConfig(b, def)
	if err != nil {
		if renameErr := os.Rename(p, p+".bad"); renameErr != nil {
			log.Printf("corrupted config %s: %v; backup failed: %v; saving disabled", p, err, renameErr)
			saveMu.Lock()
			saveBlocked = true
			saveMu.Unlock()
			return
		}
		log.Printf("corrupted config %s: %v; backed up to %s.bad", p, err, p)
		saveMu.Lock()
		saveBlocked = false
		saveMu.Unlock()
		return
	}
	blocked := false
	if len(unread) > 0 {
		// The rest loads. The next save would write the defaults over the unread
		// keys, so the file is kept as it was first.
		keys := strings.Join(unread, ", ")
		if writeErr := os.WriteFile(p+".bad", b, 0o644); writeErr != nil {
			log.Printf("config %s: not valid: %s; backup failed: %v; saving disabled", p, keys, writeErr)
			blocked = true
		} else {
			log.Printf("config %s: not valid: %s; repaired, or left at the default; the file is kept as %s.bad", p, keys, p)
		}
	}
	cfgMu.Lock()
	cfg = next
	cfgMu.Unlock()
	saveMu.Lock()
	saveBlocked = blocked
	saveMu.Unlock()
}

// parseConfig overlays the saved keys on the defaults. Each key is decoded on
// its own: decoding into the defaults would reuse their slice elements, so a
// saved scene would inherit fields it omits. unread names the keys whose value
// does not decode, which keep the default, and "scenes" when a scene in it is
// not valid.
func parseConfig(b []byte, defaults config) (c config, unread []string, err error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return defaults, nil, err
	}
	c = defaults
	// decode reports whether the key is there and its value fits target.
	decode := func(key string, target any) bool {
		v, ok := raw[key]
		if !ok {
			return false
		}
		if json.Unmarshal(v, target) != nil {
			unread = append(unread, key)
			return false
		}
		return true
	}
	var scenes ScenesConfig
	if decode("scenes", &scenes) {
		if scenes.repair() {
			unread = append(unread, "scenes")
		}
		c.Scenes = scenes
	}
	if _, ok := raw["smartPoll"]; ok {
		var s SmartPoll
		if decode("smartPoll", &s) {
			_ = s.normalize()
			c.Smart = s
		}
	} else {
		var l legacySmart
		if decode("smart", &l) {
			c.Smart = l.smartPoll()
			_ = c.Smart.normalize()
		}
	}
	decode("lighting", &c.Lighting)
	decode("notify", &c.Notify)
	decode("dpiDisplay", &c.DPIDisplay)
	decode("glass", &c.Glass)
	decode("language", &c.Language)
	var macros []Macro
	if decode("macros", &macros) && macros != nil {
		c.Macros = macros
	}
	var bindings []Binding
	if decode("bindings", &bindings) && bindings != nil {
		c.Bindings = bindings
	}
	decode("keyboards", &c.Keyboards)
	decode("viaKeys", &c.ViaKeys)
	decode("viaModels", &c.ViaModels)
	var presets []EQPreset
	if decode("eqPresets", &presets) && presets != nil {
		c.EQPresets = presets
	}
	return c, unread, nil
}

// repair makes a loaded scene pass normalize without losing the scene: each
// setting normalize refuses goes back to "leave alone", and highlight keys
// the keyboard does not have are removed. It reports whether it changed one.
func (s *Scene) repair() (changed bool) {
	reset := func(bad bool, clear func()) {
		if bad {
			clear()
			changed = true
		}
	}
	reset(s.DPI != 0 && (s.DPI < 100 || s.DPI > 50000), func() { s.DPI = 0 })
	reset(s.Lighting != nil && !slices.Contains(validEffects, s.Lighting.Effect), func() { s.Lighting = nil })
	reset(s.Brightness != nil && (*s.Brightness < 0 || *s.Brightness > 100), func() { s.Brightness = nil })
	reset(s.Volume != nil && (*s.Volume < 0 || *s.Volume > 100), func() { s.Volume = nil })
	reset(s.EQ != nil && s.EQ.EQ.Check() != nil, func() { s.EQ = nil })
	reset(s.Keyboard != nil && s.Keyboard.Check() != nil, func() { s.Keyboard = nil })
	if h := s.Highlight; h != nil {
		n := len(h.Keys)
		h.Keys = slices.DeleteFunc(h.Keys, func(k int) bool { return k < 0 || k >= leobog.Keys })
		reset(len(h.Keys) != n, func() {
			if len(h.Keys) == 0 {
				s.Highlight = nil // none of its keys exist
			}
		})
	}
	return changed
}

// repair is normalize for a loaded setup, which also fills the names and
// lists an older save lacks. A loaded file keeps its scenes: a blank name
// becomes "Scene 2" (by its place, and not a name in use), and a setting that
// is not valid is reset alone (Scene.repair). The scene switcher applies a
// scene as it is, so one that still fails normalize after that is dropped.
// It reports whether anything was changed.
func (c *ScenesConfig) repair() (changed bool) {
	c.Game.Name, c.Desktop.Name, c.Desktop.Apps, c.Desktop.Titles = "game", "desktop", nil, nil
	for _, s := range []*Scene{&c.Game, &c.Desktop} {
		changed = s.repair() || changed
		if s.normalize() != nil {
			*s = Scene{Name: s.Name}
			_ = s.normalize()
			changed = true
		}
	}
	kept := []Scene{}
	for i := range c.Scenes {
		s := &c.Scenes[i]
		if strings.TrimSpace(s.Name) == "" {
			s.Name = c.unusedName(i + 1)
			changed = true
		}
		changed = s.repair() || changed
		if s.normalize() != nil {
			changed = true
			continue
		}
		kept = append(kept, *s)
	}
	c.Scenes = kept
	return changed
}

// unusedName is "Scene n", or with the next number no custom scene has.
func (c *ScenesConfig) unusedName(n int) string {
	for ; ; n++ {
		name := tr("scene.unnamed", n)
		if !slices.ContainsFunc(c.Scenes, func(s Scene) bool { return strings.TrimSpace(s.Name) == name }) {
			return name
		}
	}
}

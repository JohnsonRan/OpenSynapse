package main

import (
	"errors"
	"slices"
	"sync"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

// LEOBOG keyboards (internal/leobog): their lighting. They show on the
// Devices page as keyboards.

type keyboardHub struct {
	mu     sync.Mutex
	devs   []*leobog.Device
	scanMu sync.Mutex
}

var keyboards keyboardHub

func (h *keyboardHub) rescan() error {
	h.scanMu.Lock()
	defer h.scanMu.Unlock()
	found, err := leobog.Scan()
	if err != nil {
		return err
	}
	for _, d := range found {
		d.SetColors(keyboardColors(d.ID))
	}
	h.mu.Lock()
	old := h.devs
	h.devs = found
	h.mu.Unlock()
	for _, d := range old {
		d.Close()
	}
	return nil
}

func (h *keyboardHub) list() []*leobog.Device {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.devs)
}

func (h *keyboardHub) get(id string) (*leobog.Device, error) {
	for _, d := range h.list() {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, errors.New("device not connected")
}

func (h *keyboardHub) infos() []DeviceInfo {
	out := []DeviceInfo{}
	for _, d := range h.list() {
		out = append(out, DeviceInfo{ID: d.ID, Name: d.Name, Category: "keyboard", Family: d.Name, Driver: "leobog", Features: razer.Features{}})
	}
	return out
}

// keyboardColors are the colors picked for a keyboard's effects, which it cannot report.
func keyboardColors(id string) leobog.Colors {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if c, ok := cfg.KeyboardColors[id]; ok {
		return c
	}
	return leobog.DefaultColors()
}

// setKeyboardLighting shows l and keeps the keyboard's colors in the config.
func setKeyboardLighting(d *leobog.Device, l leobog.Lighting) error {
	if err := d.SetLighting(l); err != nil {
		return err
	}
	cfgMu.Lock()
	if cfg.KeyboardColors == nil {
		cfg.KeyboardColors = map[string]leobog.Colors{}
	}
	changed := cfg.KeyboardColors[d.ID] != d.Colors()
	cfg.KeyboardColors[d.ID] = d.Colors()
	cfgMu.Unlock()
	if !changed {
		return nil
	}
	return saveConfig()
}

// Keyboards controls LEOBOG keyboards.
type Keyboards struct{}

// KeyboardState is the lighting shown and the effects the keyboard has.
type KeyboardState struct {
	Lighting leobog.Lighting `json:"lighting"`
	Effects  []leobog.Effect `json:"effects"`
	MaxLevel int             `json:"maxLevel"` // of brightness and speed
}

// State reads the keyboard's lighting.
func (Keyboards) State(id string) (KeyboardState, error) {
	d, err := keyboards.get(id)
	if err != nil {
		return KeyboardState{}, err
	}
	l, err := d.Lighting()
	return KeyboardState{Lighting: l, Effects: leobog.Effects, MaxLevel: leobog.MaxLevel}, err
}

// Stored reads what the keyboard keeps for an effect, to show it before switching.
func (Keyboards) Stored(id string, effect int) (leobog.Lighting, error) {
	d, err := keyboards.get(id)
	if err != nil {
		return leobog.Lighting{}, err
	}
	return d.Stored(effect)
}

// SetLighting shows l on the keyboard.
func (Keyboards) SetLighting(id string, l leobog.Lighting) error {
	d, err := keyboards.get(id)
	if err != nil {
		return err
	}
	return setKeyboardLighting(d, l)
}

// sceneKeyboards remembers what each keyboard showed before a scene set its
// lighting, so leaving the scene puts it back.
// shortcut: kept in memory only; if OpenSynapse is killed during a scene the keyboard keeps the scene's lighting.
var sceneKeyboards = sceneLights{base: map[string]leobog.Lighting{}, on: map[string]leobog.Lighting{}}

type sceneLights struct {
	sync.Mutex
	base, on map[string]leobog.Lighting // keyboard ID → before scenes / set by the scene
}

// step brings one keyboard to want, or with want nil back to its baseline.
func (sl *sceneLights) step(id string, want *leobog.Lighting, read func() (leobog.Lighting, error), write func(leobog.Lighting) error) {
	if want == nil {
		if b, ok := sl.base[id]; ok && write(b) == nil {
			delete(sl.base, id)
			delete(sl.on, id)
		}
		return
	}
	if on, ok := sl.on[id]; ok && on == *want {
		return
	}
	if _, ok := sl.base[id]; !ok {
		cur, err := read()
		if err != nil {
			return // busy or gone: try on the next tick
		}
		sl.base[id] = cur
	}
	if write(*want) == nil {
		sl.on[id] = *want
	}
}

// applySceneKeyboards brings the keyboards to the scene s; nil (no scene)
// puts back what they showed before. Cheap when nothing changes, so it runs every tick.
func applySceneKeyboards(s *Scene) {
	var want *leobog.Lighting
	if s != nil {
		want = s.Keyboard
	}
	sceneKeyboards.Lock()
	defer sceneKeyboards.Unlock()
	for _, d := range keyboards.list() {
		sceneKeyboards.step(d.ID, want, d.Lighting, func(l leobog.Lighting) error { return setKeyboardLighting(d, l) })
	}
}

// Effects lists the effects a scene can set, with the levels.
func (Keyboards) Effects() KeyboardState {
	return KeyboardState{Effects: leobog.Effects, MaxLevel: leobog.MaxLevel}
}

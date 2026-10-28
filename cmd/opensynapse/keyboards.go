package main

import (
	"encoding/json"
	"errors"
	"slices"
	"sync"

	"github.com/egoist/mygo"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

// LEOBOG keyboards (internal/leobog): their lighting. They show on the
// Devices page as keyboards.

type keyboardHub struct {
	mu      sync.Mutex
	devs    []*leobog.Device
	written map[string]*[leobog.Layers]leobog.Layer // keymap sent since the scan; nil: the remembered one
	scanMu  sync.Mutex
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
		m := keyboardMemory(d.ID)
		d.SetColors(m.Colors)
		d.SetKeyColors(m.KeyColors)
	}
	h.mu.Lock()
	old := h.devs
	h.devs = found
	h.written = map[string]*[leobog.Layers]leobog.Layer{} // unknown again: taken as the remembered keymap, a needless write at worst
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

// KeyboardMemory is what a keyboard was set to that it cannot report.
type KeyboardMemory struct {
	Colors    leobog.Colors    `json:"colors"`    // picked color per effect
	KeyColors leobog.KeyColors `json:"keyColors"` // of the custom effect
	// Keymap: the remaps without macro bindings; nil while the keyboard has its factory keys.
	Keymap *[leobog.Layers]leobog.Layer `json:"keymap,omitempty"`
	// Music: a lightshow effect drawing what the PC plays over the keyboard's own effect; "" for none.
	Music      string    `json:"music,omitempty"`
	MusicColor razer.RGB `json:"musicColor"`
}

func keyboardMemory(id string) KeyboardMemory {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if m, ok := cfg.Keyboards[id]; ok {
		return m
	}
	return KeyboardMemory{Colors: leobog.DefaultColors()}
}

// rememberKeyboard changes a keyboard's memory and saves it if it changed.
func rememberKeyboard(id string, change func(*KeyboardMemory)) error {
	cfgMu.Lock()
	if cfg.Keyboards == nil {
		cfg.Keyboards = map[string]KeyboardMemory{}
	}
	m, ok := cfg.Keyboards[id]
	if !ok {
		m = KeyboardMemory{Colors: leobog.DefaultColors()}
	}
	before, _ := json.Marshal(m)
	change(&m)
	after, _ := json.Marshal(m)
	cfg.Keyboards[id] = m
	cfgMu.Unlock()
	if string(before) == string(after) {
		return nil
	}
	return saveConfig()
}

// setKeyboardLighting shows l and keeps the keyboard's colors in the config.
func setKeyboardLighting(d *leobog.Device, l leobog.Lighting) error {
	if err := d.SetLighting(l); err != nil {
		return err
	}
	return rememberKeyboard(d.ID, func(m *KeyboardMemory) { m.Colors = d.Colors() })
}

// Keyboards controls LEOBOG keyboards.
type Keyboards struct{}

// KeyboardState is the lighting shown and the effects the keyboard has.
type KeyboardState struct {
	Lighting   leobog.Lighting `json:"lighting"`
	Effects    []leobog.Effect `json:"effects"`
	MaxLevel   int             `json:"maxLevel"` // of brightness and speed
	Music      string          `json:"music"`    // music effect drawn over the lighting, "" for none
	MusicColor razer.RGB       `json:"musicColor"`
}

// State reads the keyboard's lighting.
func (Keyboards) State(id string) (KeyboardState, error) {
	d, err := keyboards.get(id)
	if err != nil {
		return KeyboardState{}, err
	}
	l, err := d.Lighting()
	m := keyboardMemory(d.ID)
	return KeyboardState{Lighting: l, Effects: d.Effects, MaxLevel: leobog.MaxLevel, Music: m.Music, MusicColor: m.MusicColor}, err
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
		if want != nil && d.Check(*want) != nil {
			continue // an effect this model lacks
		}
		sceneKeyboards.step(d.ID, want, d.Lighting, func(l leobog.Lighting) error { return setKeyboardLighting(d, l) })
	}
}

// Effects lists the effects a scene can set, with the levels: those of the
// connected keyboard, or of the first model when none is.
// shortcut: with keyboards of two models, the scene offers the first one's effects; the other skips effects it lacks.
func (Keyboards) Effects() KeyboardState {
	m := leobog.Models[0]
	if kbs := keyboards.list(); len(kbs) > 0 {
		m = kbs[0].Model
	}
	return KeyboardState{Effects: m.Effects, MaxLevel: leobog.MaxLevel}
}

// KeyColors returns the custom effect's colors, by matrix index.
func (Keyboards) KeyColors(id string) (leobog.KeyColors, error) {
	d, err := keyboards.get(id)
	if err != nil {
		return leobog.KeyColors{}, err
	}
	return d.KeyColors(), nil
}

// SetKeyColors shows the custom effect with these colors.
func (Keyboards) SetKeyColors(id string, c leobog.KeyColors) error {
	d, err := keyboards.get(id)
	if err != nil {
		return err
	}
	if err := d.ShowKeyColors(c); err != nil {
		return err
	}
	return rememberKeyboard(d.ID, func(m *KeyboardMemory) { m.KeyColors = c })
}

// Details shows the keyboard's firmware version.
func (Keyboards) Details(id string) (DeviceDetails, error) {
	d, err := keyboards.get(id)
	if err != nil {
		return DeviceDetails{}, err
	}
	return DeviceDetails{Firmware: d.Firmware}, nil
}

// OpenSupport opens LEOBOG's download page, where the driver that updates the firmware is.
func (Keyboards) OpenSupport() error {
	return mygo.Shell.OpenExternal("https://leobogtech.com/blogs/software")
}

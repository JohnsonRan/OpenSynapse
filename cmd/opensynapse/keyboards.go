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
	written map[string]*[leobog.Layers]leobog.Layer // keymap sent since the scan; nil: the remembered one; &unknownKeymap: needs rewrite
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
	h.written = nextWritten(h.written)
	h.mu.Unlock()
	for _, d := range old {
		d.Close()
	}
	return nil
}

// nextWritten builds the keymap cache after a rescan: keymaps written before
// are dropped (taken as the remembered map), but failed writes stay marked
// unknown so a reconnecting keyboard has its keymap rewritten.
func nextWritten(old map[string]*[leobog.Layers]leobog.Layer) map[string]*[leobog.Layers]leobog.Layer {
	next := map[string]*[leobog.Layers]leobog.Layer{}
	for id, w := range old {
		if w == &unknownKeymap {
			next[id] = w
		}
	}
	return next
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
	// Notices: flash the keys on low battery, a scene change or a toggled macro.
	Notices bool `json:"notices,omitempty"`
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
// change runs under cfgMu, which the input hook takes: it must not wait on a
// device (d.mu is held for seconds during a factory reset).
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
	c := d.Colors() // not inside: that would wait on the device while holding cfgMu
	return rememberKeyboard(d.ID, func(m *KeyboardMemory) { m.Colors = c })
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
	Notices    bool            `json:"notices"` // flashes on low battery, scene changes and toggled macros
	// The key layout, for scenes that highlight keys (Effects only).
	Keys   []leobog.Key `json:"keys,omitempty"`
	Width  int          `json:"width,omitempty"`
	Height int          `json:"height,omitempty"`
}

// State reads the keyboard's lighting.
func (Keyboards) State(id string) (KeyboardState, error) {
	d, err := keyboards.get(id)
	if err != nil {
		return KeyboardState{}, err
	}
	l, err := d.Lighting()
	m := keyboardMemory(d.ID)
	return KeyboardState{Lighting: l, Effects: d.Effects, MaxLevel: leobog.MaxLevel, Music: m.Music, MusicColor: m.MusicColor, Notices: m.Notices}, err
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
var sceneKeyboards = sceneLights{base: map[string]kbShow{}, on: map[string]kbShow{}}

// kbShow is what a keyboard shows: its lighting, and with the custom effect its key colors.
type kbShow struct {
	Lighting leobog.Lighting
	Keys     leobog.KeyColors
}

type sceneLights struct {
	sync.Mutex
	base, on map[string]kbShow // keyboard ID → before scenes / set by the scene
}

// step brings one keyboard to want, or with want nil back to its baseline.
func (sl *sceneLights) step(id string, want *kbShow, read func() (kbShow, error), write func(kbShow) error) {
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

// sceneShow is what the scene shows on keyboards, nil if it sets nothing.
func sceneShow(s *Scene) *kbShow {
	switch {
	case s == nil:
		return nil
	case s.Highlight != nil:
		return &kbShow{Lighting: leobog.Lighting{Effect: leobog.EffectCustom}, Keys: s.Highlight.colors()}
	case s.Keyboard != nil:
		return &kbShow{Lighting: *s.Keyboard}
	}
	return nil
}

// applySceneKeyboards brings the keyboards to the scene s; nil (no scene)
// puts back what they showed before. Cheap when nothing changes, so it runs every tick.
func applySceneKeyboards(s *Scene) {
	want := sceneShow(s)
	sceneKeyboards.Lock()
	defer sceneKeyboards.Unlock()
	for _, d := range keyboards.list() {
		if want != nil && d.Check(want.Lighting) != nil {
			continue // an effect this model lacks
		}
		read := func() (kbShow, error) {
			l, err := d.Lighting()
			return kbShow{Lighting: l, Keys: keyboardMemory(d.ID).KeyColors}, err
		}
		sceneKeyboards.step(d.ID, want, read, func(st kbShow) error { return showOnKeyboard(d, st) })
	}
}

// showOnKeyboard writes what a scene shows, or the baseline back. The scene's
// key colors are not the user's custom colors, which the device gets back after.
func showOnKeyboard(d *leobog.Device, st kbShow) error {
	defer d.SetKeyColors(keyboardMemory(d.ID).KeyColors)
	if st.Lighting.Effect == leobog.EffectCustom {
		return d.ShowKeyColors(st.Keys)
	}
	return setKeyboardLighting(d, st.Lighting)
}

// Effects lists the effects a scene can set, with the levels: those of the
// connected keyboard, or of the first model when none is.
// shortcut: with keyboards of two models, the scene offers the first one's effects; the other skips effects it lacks.
func (Keyboards) Effects() KeyboardState {
	m := leobog.Models[0]
	if kbs := keyboards.list(); len(kbs) > 0 {
		m = kbs[0].Model
	}
	return KeyboardState{Effects: m.Effects, MaxLevel: leobog.MaxLevel, Keys: m.Layout, Width: m.Width, Height: m.Height}
}

// KeyColors returns the custom effect's colors, by matrix index; a scene
// highlighting keys does not change them.
func (Keyboards) KeyColors(id string) (leobog.KeyColors, error) {
	d, err := keyboards.get(id)
	if err != nil {
		return leobog.KeyColors{}, err
	}
	return keyboardMemory(d.ID).KeyColors, nil
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

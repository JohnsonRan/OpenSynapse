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

// sceneEffects are the keyboard effects closest to the scene's.
var sceneEffects = map[string]int{
	razer.EffectOff: leobog.EffectOff, razer.EffectStatic: 1, razer.EffectBreathing: 2,
	razer.EffectSpectrum: 3, razer.EffectWave: 13, // dreamy rainbow, sine wave
}

// sceneLighting is what a scene turns a keyboard showing cur (and keeping
// stored for the scene's effect) into; ok is false when the scene sets nothing.
func sceneLighting(s Scene, cur, stored leobog.Lighting) (leobog.Lighting, bool) {
	if s.Lighting == nil && s.Brightness == nil {
		return cur, false
	}
	l := cur
	if s.Lighting != nil {
		l = stored
		switch s.Lighting.Effect {
		case razer.EffectStatic, razer.EffectBreathing:
			l.Colorful, l.Color = false, s.Lighting.Color
		case razer.EffectSpectrum, razer.EffectWave:
			l.Colorful = true
		}
	}
	if s.Brightness != nil && l.Effect != leobog.EffectOff {
		l.Brightness = (*s.Brightness*leobog.MaxLevel + 99) / 100 // any light stays lit
	}
	return l, true
}

// applyKeyboardScene writes what the scene sets to a keyboard.
func applyKeyboardScene(d *leobog.Device, s Scene) error {
	cur, err := d.Lighting()
	if err != nil {
		return err
	}
	stored := cur
	if s.Lighting != nil {
		if stored, err = d.Stored(sceneEffects[s.Lighting.Effect]); err != nil {
			return err
		}
	}
	l, ok := sceneLighting(s, cur, stored)
	if !ok || l == cur {
		return nil
	}
	return setKeyboardLighting(d, l)
}

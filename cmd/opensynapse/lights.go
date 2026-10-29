package main

import (
	"errors"
	"log"
	"slices"
	"sync"

	"github.com/JohnsonRan/OpenSynapse/internal/evision"
	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

// Keyboards whose lighting alone the app sets (internal/evision so far), on
// the keyboard page with the BY-520 keyboards' data types and no Keys tab.

// lightKeyboard is one such keyboard, whatever its protocol.
type lightKeyboard interface {
	id() string
	name() string
	effects() []leobog.Effect
	maxLevel() int // of brightness and speed
	lighting() (leobog.Lighting, error)
	setLighting(leobog.Lighting) error
	close()
}

// lightScanners find the attached keyboards of each protocol.
var lightScanners = []func() ([]lightKeyboard, error){scanEVision}

type lightsHub struct {
	mu     sync.Mutex
	devs   []lightKeyboard
	scanMu sync.Mutex
}

var lightKeyboards lightsHub

func (h *lightsHub) rescan() error {
	h.scanMu.Lock()
	defer h.scanMu.Unlock()
	var found []lightKeyboard
	for _, scan := range lightScanners {
		ds, err := scan()
		if err != nil {
			log.Println("scan lighting keyboards:", err)
		}
		found = append(found, ds...)
	}
	h.mu.Lock()
	old := h.devs
	h.devs = found
	h.mu.Unlock()
	for _, d := range old {
		d.close()
	}
	return nil
}

func (h *lightsHub) list() []lightKeyboard {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.devs)
}

func (h *lightsHub) get(id string) (lightKeyboard, error) {
	for _, d := range h.list() {
		if d.id() == id {
			return d, nil
		}
	}
	return nil, errors.New("device not connected")
}

func (h *lightsHub) infos() []DeviceInfo {
	out := []DeviceInfo{}
	for _, d := range h.list() {
		out = append(out, DeviceInfo{ID: d.id(), Name: d.name(), Category: "keyboard", Family: d.name(), Driver: "lights", Features: razer.Features{}})
	}
	return out
}

// Lights controls keyboards whose lighting alone can be set.
type Lights struct{}

// State reads the keyboard's lighting.
func (Lights) State(id string) (KeyboardState, error) {
	d, err := lightKeyboards.get(id)
	if err != nil {
		return KeyboardState{}, err
	}
	l, err := d.lighting()
	return KeyboardState{Lighting: l, Effects: d.effects(), MaxLevel: d.maxLevel(), Colorful: true}, err
}

// Stored is the lighting with another effect: these keyboards keep one
// brightness, speed and color for all of them.
func (s Lights) Stored(id string, effect int) (leobog.Lighting, error) {
	st, err := s.State(id)
	st.Lighting.Effect = effect
	return st.Lighting, err
}

// SetLighting sets and stores the keyboard's lighting.
func (Lights) SetLighting(id string, l leobog.Lighting) error {
	d, err := lightKeyboards.get(id)
	if err != nil {
		return err
	}
	return d.setLighting(l)
}

// KeyColors: these keyboards have no per-key colors here.
func (Lights) KeyColors(id string) (leobog.KeyColors, error) { return leobog.KeyColors{}, nil }

// Keys: no key can be changed; the page hides the Keys tab.
func (Lights) Keys(id string) (KeyboardKeys, error) {
	if _, err := lightKeyboards.get(id); err != nil {
		return KeyboardKeys{}, err
	}
	return KeyboardKeys{Keys: []leobog.Key{}, FnIndex: -1, Remap: false, Reset: false, Normal: []ButtonAction{}, Fn: []ButtonAction{}, Media: mediaCodes}, nil
}

var errNoKeys = errors.New("this keyboard's keys cannot be changed")

// SetKey: no key can be changed.
func (Lights) SetKey(id string, layer, index int, act ButtonAction) error { return errNoKeys }

// ResetKeys: no key can be changed.
func (Lights) ResetKeys(id string) error { return errNoKeys }

// Details: these keyboards do not report a firmware version.
func (Lights) Details(id string) (DeviceDetails, error) { return DeviceDetails{}, nil }

// ---------- EVision ----------

type evisionKB struct{ *evision.Device }

// scanEVision leaves keyboards VIA drives alone: EVision chips also run
// VIA firmware (the Rainy 75), which then sets more than the lighting.
// The VIA keyboards are scanned first.
func scanEVision() ([]lightKeyboard, error) {
	ds, err := evision.Scan()
	var out []lightKeyboard
	for _, d := range ds {
		if _, viaErr := viaKeyboards.get(d.ID); viaErr == nil {
			d.Close()
			continue
		}
		out = append(out, evisionKB{d})
	}
	return out, err
}

func (k evisionKB) id() string    { return k.ID }
func (k evisionKB) name() string  { return k.Name }
func (k evisionKB) maxLevel() int { return evision.MaxBrightness }
func (k evisionKB) close()        { k.Close() }

func (k evisionKB) effects() []leobog.Effect {
	out := make([]leobog.Effect, len(evision.Effects))
	for i, e := range evision.Effects {
		out[i] = leobog.Effect{ID: e.ID, Name: e.Name, Speed: e.Name != "static", Color: true}
	}
	return out
}

// The firmware's speed runs from MaxSpeed (slowest) down to 0; the page's
// from 0 (slowest) up to maxLevel.
func evisionSpeed(level int) int {
	return evision.MaxSpeed - (level*evision.MaxSpeed+evision.MaxBrightness/2)/evision.MaxBrightness
}

func evisionLevel(speed int) int {
	return ((evision.MaxSpeed-speed)*evision.MaxBrightness + evision.MaxSpeed/2) / evision.MaxSpeed
}

func (k evisionKB) lighting() (leobog.Lighting, error) {
	l, err := k.Lighting()
	return leobog.Lighting{Effect: l.Effect, Brightness: l.Brightness, Speed: evisionLevel(l.Speed), Colorful: l.Random, Color: razer.RGB{R: l.R, G: l.G, B: l.B}}, err
}

func (k evisionKB) setLighting(l leobog.Lighting) error {
	if l.Speed < 0 || l.Speed > evision.MaxBrightness {
		return errors.New("speed out of range")
	}
	return k.SetLighting(evision.Lighting{Effect: l.Effect, Brightness: l.Brightness, Speed: evisionSpeed(l.Speed), Random: l.Colorful, R: l.Color.R, G: l.Color.G, B: l.Color.B})
}

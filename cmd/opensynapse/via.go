package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"slices"
	"sync"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
	"github.com/JohnsonRan/OpenSynapse/internal/razer"
	"github.com/JohnsonRan/OpenSynapse/internal/via"
)

// Keyboards with QMK and VIA (internal/via): their lighting and keys, on the
// keyboard page the BY-520 keyboards use, with the same data types. They
// keep their keymap themselves; the app remembers what a key did before it
// first changed it, which is the key's "default" and what a macro key gets
// back when the app quits.

type viaHub struct {
	mu     sync.Mutex
	devs   []*via.Device
	scanMu sync.Mutex
}

var viaKeyboards viaHub

func (h *viaHub) rescan() error {
	h.scanMu.Lock()
	defer h.scanMu.Unlock()
	found, err := via.Scan()
	if err != nil {
		return err
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

func (h *viaHub) list() []*via.Device {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.devs)
}

func (h *viaHub) get(id string) (*via.Device, error) {
	for _, d := range h.list() {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, errors.New("device not connected")
}

func (h *viaHub) infos() []DeviceInfo {
	out := []DeviceInfo{}
	for _, d := range h.list() {
		out = append(out, DeviceInfo{ID: d.ID, Name: d.Name, Category: "keyboard", Family: d.Name, Driver: "via", Features: razer.Features{}})
	}
	return out
}

// Via controls QMK keyboards with VIA.
type Via struct{}

// Lighting levels shown for VIA's 0–255 ranges.
const viaMaxLevel = 255

func toLevel(v *via.Value, raw int) int {
	if v == nil {
		return 0
	}
	return (raw*viaMaxLevel + v.Max/2) / v.Max
}

func fromLevel(v *via.Value, level int) int {
	if v == nil {
		return 0
	}
	return (level*v.Max + viaMaxLevel/2) / viaMaxLevel
}

// viaEffects are the keyboard's effects; one without an effect control has
// a single one, its only lighting.
func viaEffects(d *via.Device) []leobog.Effect {
	l := d.Light
	if l.Effect == nil {
		return []leobog.Effect{{ID: 1, Name: "static", Color: l.Color != nil}}
	}
	out := []leobog.Effect{}
	for _, e := range l.Effects {
		out = append(out, leobog.Effect{ID: e.Value, Name: e.Name, Speed: l.Speed != nil && e.Value != 0, Color: l.Color != nil && e.Value != 0})
	}
	return out
}

func viaLighting(d *via.Device, s via.State) leobog.Lighting {
	l := leobog.Lighting{Effect: s.Effect, Brightness: toLevel(d.Light.Brightness, s.Brightness), Speed: toLevel(d.Light.Speed, s.Speed), Color: hsvToRGB(s.Hue, s.Sat)}
	if d.Light.Effect == nil {
		l.Effect = 1
	}
	return l
}

// hsvToRGB and rgbToHS convert QMK's colors (hue and saturation 0–255, at full value).
func hsvToRGB(h, s byte) razer.RGB {
	hf, sf := float64(h)/255*6, float64(s)/255
	i := math.Floor(hf)
	f := hf - i
	p, q, t := 1-sf, 1-sf*f, 1-sf*(1-f)
	c := [6][3]float64{{1, t, p}, {q, 1, p}, {p, 1, t}, {p, q, 1}, {t, p, 1}, {1, p, q}}[int(i)%6]
	return razer.RGB{R: byte(math.Round(c[0] * 255)), G: byte(math.Round(c[1] * 255)), B: byte(math.Round(c[2] * 255))}
}

func rgbToHS(c razer.RGB) (byte, byte) {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	mx, mn := max(r, g, b), min(r, g, b)
	if mx == 0 {
		return 0, 0
	}
	d := mx - mn
	var h float64
	switch {
	case d == 0:
	case mx == r:
		h = math.Mod((g-b)/d, 6)
	case mx == g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	if h < 0 {
		h += 6
	}
	return byte(math.Round(h/6*255)) % 255, byte(math.Round(d / mx * 255))
}

// State reads the keyboard's lighting.
func (Via) State(id string) (KeyboardState, error) {
	d, err := viaKeyboards.get(id)
	if err != nil {
		return KeyboardState{}, err
	}
	st := KeyboardState{Effects: []leobog.Effect{}, MaxLevel: viaMaxLevel}
	if !d.HasLighting() {
		return st, nil
	}
	s, err := d.Lighting()
	st.Lighting, st.Effects = viaLighting(d, s), viaEffects(d)
	return st, err
}

// Stored is the lighting with another effect: the keyboard keeps one
// brightness, speed and color for all of them.
func (v Via) Stored(id string, effect int) (leobog.Lighting, error) {
	st, err := v.State(id)
	st.Lighting.Effect = effect
	return st.Lighting, err
}

// SetLighting sets and stores the keyboard's lighting.
func (Via) SetLighting(id string, l leobog.Lighting) error {
	d, err := viaKeyboards.get(id)
	if err != nil {
		return err
	}
	if !d.HasLighting() {
		return errors.New("this keyboard's lighting cannot be changed")
	}
	h, s := rgbToHS(l.Color)
	return d.SetLighting(via.State{Effect: l.Effect, Brightness: fromLevel(d.Light.Brightness, l.Brightness), Speed: fromLevel(d.Light.Speed, l.Speed), Hue: h, Sat: s})
}

// KeyColors: VIA keyboards have no per-key colors.
func (Via) KeyColors(id string) (leobog.KeyColors, error) { return leobog.KeyColors{}, nil }

// Details: VIA does not report a firmware version.
func (Via) Details(id string) (DeviceDetails, error) { return DeviceDetails{}, nil }

// ---------- keys ----------

// viaKeyOf names a key's place in the remembered originals.
func viaKeyOf(layer, index int) string { return fmt.Sprintf("%d:%d", layer, index) }

// viaOriginal is what a key did before the app first changed it, ok false if unchanged.
func viaOriginal(id string, layer, index int) (uint16, bool) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	c, ok := cfg.ViaKeys[id][viaKeyOf(layer, index)]
	return c, ok
}

// rememberOriginal keeps what a key does before the app's first change to it.
func rememberOriginal(id string, layer, index int, code uint16) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if cfg.ViaKeys == nil {
		cfg.ViaKeys = map[string]map[string]uint16{}
	}
	if cfg.ViaKeys[id] == nil {
		cfg.ViaKeys[id] = map[string]uint16{}
	}
	if _, ok := cfg.ViaKeys[id][viaKeyOf(layer, index)]; !ok {
		cfg.ViaKeys[id][viaKeyOf(layer, index)] = code
	}
}

func forgetOriginal(id string, layer, index int) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	delete(cfg.ViaKeys[id], viaKeyOf(layer, index))
}

// triggerCode is the keycode of a macro trigger key: F13–F24 are HID usages 0x68–0x73.
func triggerCode(vk int) uint16 { return uint16(0x68 + vk - firstTrigger) }

func viaKeyAction(code uint16, def uint16) ButtonAction {
	if code == def {
		return ButtonAction{Kind: "default"}
	}
	if u, ok := via.Media(code); ok && slices.Contains(mediaCodes, u) {
		return ButtonAction{Kind: "media", Code: u}
	}
	if mods, u, ok := via.Basic(code); ok {
		if vk, ok := hidToVK[u]; ok || u == 0 {
			return ButtonAction{Kind: "key", Code: vk, Mods: mods}
		}
	}
	return ButtonAction{Kind: "other"}
}

func viaCodeFor(act ButtonAction) (uint16, error) {
	switch act.Kind {
	case "key":
		u, ok := vkToHID[act.Code]
		if (ok || act.Code == 0) && act.Mods >= 0 && act.Mods <= 15 && (act.Code != 0 || act.Mods != 0) && !isTriggerVK(act.Code) {
			if act.Code == 0 { // modifiers alone: the first of them as a key
				for i := range 4 {
					if act.Mods&(1<<i) != 0 {
						return via.BasicCode(act.Mods&^(1<<i), byte(0xE0+i)), nil
					}
				}
			}
			return via.BasicCode(act.Mods, u), nil
		}
	case "media":
		if c := via.MediaCode(act.Code); c != 0 && slices.Contains(mediaCodes, act.Code) {
			return c, nil
		}
	}
	return 0, errors.New(tr("err.buttonAction"))
}

// viaLayers are the layers shown: normal and Fn, as many as the keyboard has.
func viaLayers(d *via.Device) int { return min(d.Layers, 2) }

// Keys returns the keyboard's keys and what they do.
func (Via) Keys(id string) (KeyboardKeys, error) {
	d, err := viaKeyboards.get(id)
	if err != nil {
		return KeyboardKeys{}, err
	}
	var maps [2][]uint16
	for l := range viaLayers(d) {
		if maps[l], err = d.Keymap(l); err != nil {
			return KeyboardKeys{}, err
		}
	}
	out := KeyboardKeys{Width: d.Width, Height: d.Height, FnIndex: -1, Remap: true, Media: mediaCodes, Keys: []leobog.Key{}}
	cfgMu.Lock()
	bs := slices.Clone(cfg.Bindings)
	cfgMu.Unlock()
	for _, k := range d.Keys {
		i := d.Index(k.Row, k.Col)
		code := maps[0][i]
		if l, ok := via.Momentary(code, d.Protocol); ok && l == 1 && out.FnIndex < 0 {
			out.FnIndex = i
		}
		out.Keys = append(out.Keys, leobog.Key{Index: i, Name: via.Label(code, d.Protocol), X: k.X, Y: k.Y, W: k.W, H: k.H})
		for l, dst := range []*[]ButtonAction{&out.Normal, &out.Fn} {
			act := ButtonAction{Kind: "other"}
			if maps[l] != nil {
				def, ok := viaOriginal(d.ID, l, i)
				if !ok {
					def = maps[l][i]
				}
				act = viaKeyAction(maps[l][i], def)
			}
			if j := slices.IndexFunc(bs, func(b Binding) bool { return b.Family == d.Name && b.Button == i && b.Layer == l }); j >= 0 {
				act = ButtonAction{Kind: "macro", Macro: bs[j].Macro}
			}
			*dst = append(*dst, act)
		}
	}
	return out, nil
}

// viaKey finds a drawn key by its index.
func viaKey(d *via.Device, index int) (via.Key, bool) {
	i := slices.IndexFunc(d.Keys, func(k via.Key) bool { return d.Index(k.Row, k.Col) == index })
	if i < 0 {
		return via.Key{}, false
	}
	return d.Keys[i], true
}

// setViaKey writes a key, keeping what it did before the app's first change.
func setViaKey(d *via.Device, layer int, k via.Key, code uint16) error {
	cur, err := d.Keycode(layer, k.Row, k.Col)
	if err != nil || cur == code {
		return err
	}
	rememberOriginal(d.ID, layer, d.Index(k.Row, k.Col), cur)
	return d.SetKeycode(layer, k.Row, k.Col, code)
}

// SetKey sets what a key does on a layer (0 normal, 1 with Fn).
func (Via) SetKey(id string, layer, index int, act ButtonAction) error {
	bindMu.Lock()
	defer bindMu.Unlock()
	d, err := viaKeyboards.get(id)
	if err != nil {
		return err
	}
	k, ok := viaKey(d, index)
	if !ok || layer < 0 || layer >= viaLayers(d) {
		return errors.New(tr("err.buttonAction"))
	}
	if c, err := d.Keycode(0, k.Row, k.Col); err == nil {
		if l, ok := via.Momentary(c, d.Protocol); ok && l == 1 && layer == 0 {
			return errors.New(tr("err.buttonAction")) // the Fn key switches the layer
		}
	}
	find := func() int {
		return slices.IndexFunc(cfg.Bindings, func(b Binding) bool { return b.Family == d.Name && b.Button == index && b.Layer == layer })
	}
	var code uint16
	switch act.Kind {
	case "macro":
		cfgMu.Lock()
		if !slices.ContainsFunc(cfg.Macros, func(m Macro) bool { return m.ID == act.Macro }) {
			cfgMu.Unlock()
			return errors.New(tr("err.macroMissing"))
		}
		trig := 0
		if i := find(); i >= 0 {
			cfg.Bindings[i].Macro = act.Macro
			trig = cfg.Bindings[i].Trigger
		} else if trig = freeTrigger(cfg.Bindings); trig != 0 {
			cfg.Bindings = append(cfg.Bindings, Binding{Family: d.Name, Button: index, Layer: layer, Macro: act.Macro, Trigger: trig})
		} else {
			cfgMu.Unlock()
			return errors.New(tr("err.noTrigger", numTriggers))
		}
		cfgMu.Unlock()
		code = triggerCode(trig)
	case "default":
		orig, ok := viaOriginal(d.ID, layer, index)
		if !ok {
			return nil // never changed by the app
		}
		code = orig
	default:
		if code, err = viaCodeFor(act); err != nil {
			return err
		}
	}
	if act.Kind != "macro" {
		cfgMu.Lock()
		if i := find(); i >= 0 {
			cfg.Bindings = slices.Delete(cfg.Bindings, i, i+1)
		}
		cfgMu.Unlock()
	}
	refreshHooks()
	if err := setViaKey(d, layer, k, code); err != nil {
		return err
	}
	if act.Kind == "default" {
		forgetOriginal(d.ID, layer, index)
	}
	return saveConfig()
}

// ResetKeys gives every key the firmware's keymap back and unbinds its macros.
func (Via) ResetKeys(id string) error {
	bindMu.Lock()
	defer bindMu.Unlock()
	d, err := viaKeyboards.get(id)
	if err != nil {
		return err
	}
	if err := d.ResetKeymap(); err != nil {
		return err
	}
	cfgMu.Lock()
	cfg.Bindings = slices.DeleteFunc(cfg.Bindings, func(b Binding) bool { return b.Family == d.Name })
	delete(cfg.ViaKeys, d.ID)
	cfgMu.Unlock()
	refreshHooks()
	return saveConfig()
}

// syncViaBindings makes macro keys send their trigger, or with restore what
// they did before; bindMu is held.
func syncViaBindings(restore bool) {
	cfgMu.Lock()
	bs := slices.Clone(cfg.Bindings)
	cfgMu.Unlock()
	for _, d := range viaKeyboards.list() {
		for _, b := range bs {
			k, ok := viaKey(d, b.Button)
			if b.Family != d.Name || !ok || b.Layer >= viaLayers(d) {
				continue
			}
			want := triggerCode(b.Trigger)
			if restore {
				orig, ok := viaOriginal(d.ID, b.Layer, b.Button)
				if !ok {
					continue
				}
				want = orig
			}
			if err := setViaKey(d, b.Layer, k, want); err != nil {
				log.Println("key", b.Button, "of", d.ID, err)
			}
		}
	}
}

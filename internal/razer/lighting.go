package razer

import (
	"errors"
	"slices"
	"strings"
)

// Lighting effects offered across devices.
const (
	EffectOff       = "off"
	EffectStatic    = "static"
	EffectBreathing = "breathing"
	EffectSpectrum  = "spectrum"
	EffectWave      = "wave"
)

// LightingCaps describes what the device's lighting can do as a whole.
type LightingCaps struct {
	Effects    []string `json:"effects"`
	Color      bool     `json:"color"` // static and breathing take a color
	Brightness bool     `json:"brightness"`
}

// Command families, most capable first. openrazer picks one per device.
const (
	famExtended = "extended_matrix_effect_"       // class 0x0F, per zone
	famMouseExt = "mouse_extended_matrix_effect_" // class 0x03 cmd 0x0D, per zone
	famStandard = "standard_matrix_effect_"       // class 0x03 cmd 0x0A, whole device
	famClassic  = "standard_set_led_effect"       // effect id per zone, no color
	famState    = "standard_set_led_state"        // on/off per zone
)

func (m *Model) lightingFamily() string {
	for _, f := range []string{famExtended, famMouseExt, famStandard} {
		if m.hasPrefix(f) {
			return f
		}
	}
	// Extended keyboards also declare classic/state commands for their macro
	// and game-mode LEDs; only devices without a matrix use these for lighting.
	if len(m.zonesFor(famClassic)) > 0 {
		return famClassic
	}
	if len(m.zonesFor(famState)) > 0 {
		return famState
	}
	return ""
}

func (m *Model) brightnessCmd() string {
	for _, b := range []string{"extended_matrix_brightness", "standard_set_led_brightness"} {
		if m.has(b) {
			return b
		}
	}
	return ""
}

func (m *Model) lightingCaps() *LightingCaps {
	f := m.lightingFamily()
	if f == "" {
		return nil
	}
	c := &LightingCaps{Brightness: m.brightnessCmd() != ""}
	switch f {
	case famClassic:
		c.Effects = []string{EffectStatic, EffectBreathing, EffectSpectrum}
	case famState:
		c.Effects = []string{EffectOff, EffectStatic}
	default:
		c.Color = true
		// Static is used by openrazer for every matrix device, but the tables
		// miss it (its builder takes a pointer), so it is always offered.
		c.Effects = []string{EffectOff, EffectStatic}
		for _, e := range []struct{ name, builder string }{
			{EffectBreathing, "breathing_single"}, {EffectSpectrum, "spectrum"}, {EffectWave, "wave"},
		} {
			if m.has(f + e.builder) {
				c.Effects = append(c.Effects, e.name)
			}
		}
	}
	return c
}

// Builder name suffix per effect.
var effectBuilder = map[string]string{EffectOff: "none", EffectStatic: "static", EffectBreathing: "breathing_single",
	EffectSpectrum: "spectrum", EffectWave: "wave"}

// fillLightingGaps gives commands the tables leave out the transaction id of
// their siblings: static effects (openrazer builds them from a pointer, which
// the extraction misses) and brightness reads.
func (m *Model) fillLightingGaps() {
	for _, f := range []string{famExtended, famMouseExt, famStandard} {
		for k, v := range m.tx {
			if strings.HasPrefix(k, f) && !m.has(f+"static") {
				m.tx[f+"static"] = v
			}
		}
	}
	for get, set := range map[string]string{"extended_matrix_get_brightness": "extended_matrix_brightness", "standard_get_led_brightness": "standard_set_led_brightness"} {
		if v, ok := m.tx[set]; ok && !m.has(get) {
			m.tx[get] = v
		}
	}
}

// Zones that are not the device's main lighting.
var auxZones = []byte{0x07, 0x08, 0x0C, 0x0D, 0x0E, 0x20, 0x21, 0x22} // macro, game, profile, charging LEDs

// zonesFor lists the LED zones the family's builders address. A builder whose
// prefix carries only the storage byte addresses several zones; those come
// from the openrazer daemon's method names (set_logo_static, set_scroll_...).
func (m *Model) zonesFor(family string) []byte {
	var zones []byte
	perZone := false
	for name, list := range m.prefixes {
		if !strings.HasPrefix(name, family) {
			continue
		}
		for _, p := range list {
			switch {
			case len(p) >= 2 && !slices.Contains(auxZones, p[1]):
				zones = append(zones, p[1])
			case len(p) == 1:
				perZone = true
			}
		}
	}
	// Builders that take the LED as a parameter name no zone; the daemon's
	// methods (set_logo_static, set_scroll_on, ...) say which exist.
	if perZone || (len(zones) == 0 && m.hasPrefix(family)) {
		for word, z := range map[string]byte{"logo": 0x04, "scroll": 0x01, "left": 0x11, "right": 0x10, "backlight": 0x05} {
			for meth := range m.methods {
				if strings.HasPrefix(meth, "set_"+word+"_") {
					zones = append(zones, z)
					break
				}
			}
		}
	}
	slices.Sort(zones)
	zones = slices.Compact(zones)
	if slices.Contains(zones, 0x00) {
		return []byte{0x00} // ZERO_LED: the whole device at once
	}
	return zones
}

// lightingZones returns the zones to address, with a sensible default.
func (m *Model) lightingZones(family string) []byte {
	if z := m.zonesFor(family); len(z) > 0 {
		return z
	}
	if m.Category == "keyboard" {
		return []byte{0x05} // BACKLIGHT_LED
	}
	return []byte{0x00}
}

// RGB is a color.
type RGB struct {
	R uint8 `json:"r"`
	G uint8 `json:"g"`
	B uint8 `json:"b"`
}

// SetLighting applies an effect to every lighting zone of the device.
func (d *Device) SetLighting(effect string, c RGB) error {
	caps := d.lightingCaps()
	if caps == nil || !slices.Contains(caps.Effects, effect) {
		return ErrUnsupported
	}
	f := d.lightingFamily()
	txName := f + effectBuilder[effect]
	if f == famStandard {
		var args []byte
		var size byte
		switch effect {
		case EffectOff:
			args, size = []byte{0x00}, 1
		case EffectWave:
			args, size = []byte{0x01, 0x01}, 2
		case EffectBreathing:
			args, size = []byte{0x03, 0x01, c.R, c.G, c.B}, 8
		case EffectSpectrum:
			args, size = []byte{0x04}, 1
		case EffectStatic:
			args, size = []byte{0x06, c.R, c.G, c.B}, 4
		}
		_, err := d.query(txName, 0x03, 0x0A, size, args...)
		return err
	}
	var errs []error
	for _, z := range d.lightingZones(f) {
		var err error
		switch f {
		case famExtended:
			var args []byte
			switch effect {
			case EffectOff:
				args = []byte{varStore, z, 0x00, 0, 0, 0}
			case EffectStatic:
				args = []byte{varStore, z, 0x01, 0, 0, 0x01, c.R, c.G, c.B}
			case EffectBreathing:
				args = []byte{varStore, z, 0x02, 0x01, 0, 0x01, c.R, c.G, c.B}
			case EffectSpectrum:
				args = []byte{varStore, z, 0x03, 0, 0, 0}
			case EffectWave:
				args = []byte{varStore, z, 0x04, 0x01, 0x28, 0}
			}
			_, err = d.query(txName, 0x0F, 0x02, byte(len(args)), args...)
		case famMouseExt:
			var args []byte
			switch effect {
			case EffectOff:
				args = []byte{varStore, z, 0x00}
			case EffectStatic:
				args = []byte{varStore, z, 0x06, c.R, c.G, c.B}
			case EffectBreathing:
				args = []byte{varStore, z, 0x03, 0x01, c.R, c.G, c.B, 0, 0, 0}
			case EffectSpectrum:
				args = []byte{varStore, z, 0x04}
			}
			_, err = d.query(txName, 0x03, 0x0D, byte(len(args)), args...)
		case famClassic:
			id := map[string]byte{EffectStatic: 0x00, EffectBreathing: 0x02, EffectSpectrum: 0x04}[effect]
			_, err = d.query(famClassic, 0x03, 0x02, 0x03, varStore, z, id)
		case famState:
			on := byte(0)
			if effect != EffectOff {
				on = 1
			}
			_, err = d.query(famState, 0x03, 0x00, 0x03, varStore, z, on)
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (d *Device) brightnessZones() []byte {
	if z := d.zonesFor(d.brightnessCmd()); len(z) > 0 {
		return z
	}
	return d.lightingZones(d.lightingFamily())
}

// Brightness returns 0–100, read from the first lighting zone.
func (d *Device) Brightness() (int, error) {
	b := d.brightnessCmd()
	z := d.brightnessZones()
	if b == "" || len(z) == 0 {
		return 0, ErrUnsupported
	}
	var r Report
	var err error
	if b == "extended_matrix_brightness" {
		r, err = d.query("extended_matrix_get_brightness", 0x0F, 0x84, 0x03, varStore, z[0])
	} else {
		r, err = d.query("standard_get_led_brightness", 0x03, 0x83, 0x03, varStore, z[0])
	}
	if err != nil {
		return 0, err
	}
	return (int(r.Args()[2])*100 + 127) / 255, nil
}

// SetBrightness sets every lighting zone to pct (0–100).
func (d *Device) SetBrightness(pct int) error {
	b := d.brightnessCmd()
	if b == "" {
		return ErrUnsupported
	}
	if pct < 0 || pct > 100 {
		return errors.New("brightness must be 0–100")
	}
	v := byte((pct*255 + 50) / 100)
	var errs []error
	for _, z := range d.brightnessZones() {
		var err error
		if b == "extended_matrix_brightness" {
			_, err = d.query(b, 0x0F, 0x04, 0x03, varStore, z, v)
		} else {
			_, err = d.query(b, 0x03, 0x03, 0x03, varStore, z, v)
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

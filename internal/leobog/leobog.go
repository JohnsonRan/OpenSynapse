// Package leobog controls the lighting of LEOBOG keyboards built on Sinowealth
// chips. The protocol is what the official LEOBOG ONE driver sends, recorded
// from its HID calls; nothing is guessed. Only the Hi75 on its cable has been tried.
//
// Every report is a 520-byte feature report, ID 0x06, on collection COL06:
// [0x06, cmd, 0, 0, 1, 0, length, 0, data...]. A read is a SetFeature with
// cmd|0x80 and a GetFeature for the answer. The driver changes lighting by
// reading the 128-byte config block, changing it and writing it back whole,
// then always writing the color palette.
package leobog

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

const (
	reportLen = 520
	reportID  = 0x06

	cmdHello   = 0x82 // the driver's first request; its answer tells our firmware
	cmdConfig  = 0x04 // config block; read with cmdConfig|0x80
	cmdPalette = 0x0a // write only: the driver never reads it back
	readFlag   = 0x80

	configLen = 0x80
)

// Config block offsets, in the whole report.
const (
	offEffect = 18
	offParams = 64 // 2 bytes per effect ID: brightness, speed<<4 | color slot
	offMagic  = 134
)

// Palette block: 7 colors per effect ID from 1 to 13; the other IDs have none.
const (
	offPalette     = 8
	paletteSlots   = 7
	paletteEffects = 14 // IDs 0–13
	offPaletteEnd  = 514
)

var magic = []byte{0x5a, 0xa5}

// hello is the answer of the firmware this was recorded on. Other keyboards
// share the 258A:010C ID with a different layout, so they are left alone.
// shortcut: exact match on the Hi75's answer; relax it once another model is tried.
var hello = []byte{0x06, 0x82, 0x01, 0x00, 0x01, 0x00, 0x06, 0x00, 0x03, 0x00, 0x00, 0x00, 0x00, 0x9b}

// Lighting levels go from 0 to MaxLevel; brightness 0 turns the effect dark.
const MaxLevel = 4

// colorSel values: a palette slot, or every color at once.
const (
	slotCustom   = 0 // where a picked color goes
	slotColorful = 7
)

// Effect is one hardware effect, in the order the driver lists them.
type Effect struct {
	ID    int    `json:"id"`
	Name  string `json:"name"` // stable key for translations
	Speed bool   `json:"speed"`
	Color bool   `json:"color"` // takes a single color besides colorful
}

// EffectOff is the effect ID with every key dark.
const EffectOff = 0

// Effects are the Hi75's, as the driver lists them. IDs 9, 14 and 18–20 exist
// on related boards but not here; 21 (per-key colors) needs its own report.
var Effects = []Effect{
	{1, "static", false, true},
	{2, "breathing", true, true},
	{3, "rainbow", true, true},
	{4, "trigger", true, true},
	{5, "raindrops", true, true},
	{6, "rainbowWheel", true, true},
	{7, "ripples", true, true},
	{8, "stars", true, true},
	{10, "stream", true, true},
	{11, "drift", true, true},
	{12, "reactive", true, true},
	{13, "sineWave", true, true},
	{15, "windmill", true, false},
	{16, "waterfall", true, false},
	{17, "blossom", true, false},
	{EffectOff, "off", false, false},
}

// Lookup finds an effect by ID.
func Lookup(id int) (Effect, bool) {
	for _, e := range Effects {
		if e.ID == id {
			return e, true
		}
	}
	return Effect{}, false
}

// Lighting is what the keyboard shows. Brightness and Speed are 0–MaxLevel;
// Colorful cycles colors, otherwise Color is used.
type Lighting struct {
	Effect     int       `json:"effect"`
	Brightness int       `json:"brightness"`
	Speed      int       `json:"speed"`
	Colorful   bool      `json:"colorful"`
	Color      razer.RGB `json:"color"`
}

// Colors are the picked color per effect ID. The keyboard cannot report them
// and every write sends all of them, so the app keeps them.
type Colors [paletteEffects]razer.RGB

// defaultSlots are the driver's palette for every effect; slot 0 is replaced
// by a picked color.
var defaultSlots = [paletteSlots]razer.RGB{
	{R: 0xff}, {G: 0xff}, {B: 0xff}, {R: 0xff, G: 0xff}, {R: 0xff, B: 0xff}, {G: 0xff, B: 0xff}, {R: 0xff, G: 0xff, B: 0xff},
}

// DefaultColors is the palette a keyboard has before a color is picked.
func DefaultColors() Colors {
	var c Colors
	for i := range c {
		c[i] = defaultSlots[0]
	}
	return c
}

// port carries feature reports; tests replace it.
type port interface {
	set(report []byte) error
	get() ([]byte, error)
}

// Device is a connected keyboard.
type Device struct {
	ID, Name string
	mu       sync.Mutex
	port     port
	colors   Colors
}

func header(cmd, length, last byte) []byte {
	r := make([]byte, reportLen)
	copy(r, []byte{reportID, cmd, 0, 0, 1, 0, length, last})
	return r
}

// request sends a read and returns its answer, checked to be that read's.
func (d *Device) request(req []byte) ([]byte, error) {
	cmd := req[1]
	if err := d.port.set(req); err != nil {
		return nil, err
	}
	r, err := d.port.get()
	if err != nil {
		return nil, err
	}
	if len(r) != reportLen || r[0] != reportID || r[1] != cmd {
		return nil, errors.New("keyboard answered another request")
	}
	return r, nil
}

// identify checks that the keyboard answers like the recorded firmware.
func (d *Device) identify() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	req := make([]byte, reportLen)
	copy(req, hello[:7]) // the driver's request: the answer's first 7 bytes
	r, err := d.request(req)
	if err != nil {
		return err
	}
	if !bytes.Equal(r[:len(hello)], hello) {
		return fmt.Errorf("unknown firmware % x", r[:len(hello)])
	}
	return nil
}

func (d *Device) readConfig() ([]byte, error) {
	r, err := d.request(header(cmdConfig|readFlag, configLen, 0))
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(r[offMagic:offMagic+2], magic) {
		return nil, errors.New("config block not recognized")
	}
	return r, nil
}

// decode reads the settings of an effect out of a config block.
func decode(cfg []byte, colors *Colors, effect int) Lighting {
	l := Lighting{Effect: effect}
	if l.Effect == EffectOff || offParams+2*l.Effect+1 >= offMagic {
		return l
	}
	p := cfg[offParams+2*l.Effect:]
	l.Brightness, l.Speed = int(p[0]), int(p[1]>>4)
	switch slot := p[1] & 0x0f; {
	case slot == slotColorful:
		l.Colorful = true
	case slot == slotCustom && l.Effect < paletteEffects:
		l.Color = colors[l.Effect]
	case int(slot) < paletteSlots:
		l.Color = defaultSlots[slot]
	}
	return l
}

// encode changes a config block read from the keyboard into the block to
// write back; bytes it does not know stay as read.
func encode(cfg []byte, l Lighting) []byte {
	w := make([]byte, reportLen)
	copy(w, cfg[:offMagic+2])
	w[1] = cmdConfig
	w[offEffect] = byte(l.Effect)
	if l.Effect == EffectOff {
		return w
	}
	slot := byte(slotColorful)
	if !l.Colorful {
		slot = slotCustom
	}
	w[offParams+2*l.Effect] = byte(l.Brightness)
	w[offParams+2*l.Effect+1] = byte(l.Speed)<<4 | slot
	return w
}

// palette builds the palette report: the driver's colors, with each effect's
// picked color in slot 0.
func palette(c *Colors) []byte {
	w := header(cmdPalette, 0, 2)
	for e := 1; e < paletteEffects; e++ {
		for s, rgb := range defaultSlots {
			if s == slotCustom {
				rgb = c[e]
			}
			i := offPalette + (e*paletteSlots+s)*3
			w[i], w[i+1], w[i+2] = rgb.R, rgb.G, rgb.B
		}
	}
	copy(w[offPaletteEnd:], magic)
	return w
}

// Check reports whether the keyboard accepts l.
func (l Lighting) Check() error {
	e, ok := Lookup(l.Effect)
	switch {
	case !ok:
		return fmt.Errorf("unknown effect %d", l.Effect)
	case l.Brightness < 0 || l.Brightness > MaxLevel || l.Speed < 0 || l.Speed > MaxLevel:
		return fmt.Errorf("brightness and speed must be 0–%d", MaxLevel)
	case !l.Colorful && !e.Color && l.Effect != EffectOff:
		return errors.New("this effect only cycles colors")
	}
	return nil
}

// Colors returns the picked colors the keyboard has.
func (d *Device) Colors() Colors {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.colors
}

// SetColors tells the device the colors picked before, which it cannot report.
func (d *Device) SetColors(c Colors) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.colors = c
}

// Lighting reads what the keyboard shows.
func (d *Device) Lighting() (Lighting, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cfg, err := d.readConfig()
	if err != nil {
		return Lighting{}, err
	}
	return decode(cfg, &d.colors, int(cfg[offEffect])), nil
}

// Stored reads the settings the keyboard keeps for an effect, shown or not.
func (d *Device) Stored(effect int) (Lighting, error) {
	if _, ok := Lookup(effect); !ok {
		return Lighting{}, fmt.Errorf("unknown effect %d", effect)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	cfg, err := d.readConfig()
	if err != nil {
		return Lighting{}, err
	}
	return decode(cfg, &d.colors, effect), nil
}

// SetLighting shows l, keeping what the other effects are set to.
func (d *Device) SetLighting(l Lighting) error {
	if err := l.Check(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	cfg, err := d.readConfig()
	if err != nil {
		return err
	}
	colors := d.colors
	if !l.Colorful && l.Effect != EffectOff {
		colors[l.Effect] = l.Color
	}
	if err := d.port.set(encode(cfg, l)); err != nil {
		return err
	}
	if err := d.port.set(palette(&colors)); err != nil {
		return err
	}
	d.colors = colors
	return nil
}

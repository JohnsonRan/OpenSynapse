// Package leobog controls LEOBOG keyboards built on Sinowealth chips:
// lighting, per-key colors and key remaps. The protocol is what the official
// LEOBOG ONE driver sends, recorded from its HID calls; nothing is guessed.
// What differs between keyboards is in their Model; only the Hi75 on its
// cable has been tried.
//
// Every report is a 520-byte feature report, ID 0x06, on collection COL06:
// [0x06, cmd, 0, 0, 1, 0, length, 0, data...]. A read is a SetFeature with
// cmd|0x80 and a GetFeature for the answer. The driver changes lighting by
// reading the 128-byte config block, changing it and writing it back whole,
// then always writing the color palette.
package leobog

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

const (
	reportLen = 520
	reportID  = 0x06

	cmdHello   = 0x82 // the driver's first request; its answer tells our firmware
	cmdKeymap  = 0x03 // one layer's remaps; write only
	cmdConfig  = 0x04 // config block; read with cmdConfig|0x80
	cmdKeys    = 0x06 // per-key colors of the custom effect; write only
	cmdFrame   = 0x08 // per-key colors shown until the next frame (music effects); not stored
	cmdPalette = 0x0a // write only: the driver never reads it back
	readFlag   = 0x80

	configLen = 0x80
)

// Config block offsets, in the whole report.
const (
	offCustom = 17 // 1 while the custom effect shows the per-key colors
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

// helloRequest is the driver's first request; a model's Hello is the answer.
var helloRequest = []byte{reportID, cmdHello, 0x01, 0x00, 0x01, 0x00, 0x06}

// Model is one keyboard: how to recognize it and what it has.
type Model struct {
	Name     string
	VID, PID uint16
	// Hello is the answer of the firmware the model was recorded with. Other
	// keyboards share Sinowealth IDs with another layout, so they are left alone.
	Hello   []byte
	Effects []Effect // in the order the driver lists them
	Layout  []Key
	Width   int // of the drawing Layout is placed on
	Height  int
	FnIndex int // the Fn key's matrix index: it switches layers and cannot be remapped
	// defaultLayers are the factory remaps as the driver writes them after
	// restoring defaults: 4 bytes per matrix index, one hex string per layer.
	defaultLayers [Layers]string
	// What the driver's factory reset writes besides: the config block
	// (bytes 8–133), and the keys the custom effect lights white.
	factoryConfig string
	factoryKeys   []int
}

// Models are the keyboards this package drives.
var Models = []*Model{hi75}

// Key is one key: the matrix index the firmware uses for colors and remaps,
// and its rectangle on the model's drawing.
type Key struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	W     int    `json:"w"`
	H     int    `json:"h"`
}

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

// EffectOff is the effect ID with every key dark; EffectCustom shows the
// per-key colors.
const (
	EffectOff    = 0
	EffectCustom = 21
)

// Effect finds one of the model's effects by ID.
func (m *Model) Effect(id int) (Effect, bool) {
	for _, e := range m.Effects {
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
	*Model
	ID, Name  string
	Firmware  string // e.g. 5.0.0, "" if unknown
	mu        sync.Mutex
	port      port
	colors    Colors
	keyColors KeyColors
}

func header(cmd, length, last byte) []byte {
	r := make([]byte, reportLen)
	copy(r, []byte{reportID, cmd, 0, 0, 1, 0, length, last})
	return r
}

// busyWait and busyTries: still storing a write (a factory reset is 7), the
// keyboard answers a read with that write; it is asked again a moment later.
var (
	busyWait  = 100 * time.Millisecond
	busyTries = 5
)

// request sends a read and returns its answer, checked to be that read's.
func (d *Device) request(req []byte) ([]byte, error) {
	cmd := req[1]
	for try := 1; ; try++ {
		if err := d.port.set(req); err != nil {
			return nil, err
		}
		r, err := d.port.get()
		if err != nil {
			return nil, err
		}
		if len(r) == reportLen && r[0] == reportID && r[1] == cmd {
			return r, nil
		}
		if try == busyTries {
			return nil, errors.New("keyboard answered another request")
		}
		time.Sleep(busyWait)
	}
}

// identify checks that the keyboard answers like the recorded firmware.
func (d *Device) identify() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	req := make([]byte, reportLen)
	copy(req, helloRequest)
	r, err := d.request(req)
	if err != nil {
		return err
	}
	if !bytes.Equal(r[:len(d.Hello)], d.Hello) {
		return fmt.Errorf("unknown firmware % x", r[:len(d.Hello)])
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
	// The custom effect's levels are not the 0–4 the driver offers; it leaves them alone.
	if l.Effect == EffectOff || l.Effect == EffectCustom || offParams+2*l.Effect+1 >= offMagic {
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
	w[offCustom] = 0
	if l.Effect == EffectCustom {
		w[offCustom] = 1
	}
	if l.Effect == EffectOff || l.Effect == EffectCustom {
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

// Check reports whether a keyboard of some model accepts l; a scene stores
// lighting before knowing which keyboard it meets.
func (l Lighting) Check() error {
	var err error
	for _, m := range Models {
		if err = m.Check(l); err == nil {
			return nil
		}
	}
	return err
}

// Check reports whether the model accepts l.
func (m *Model) Check(l Lighting) error {
	e, ok := m.Effect(l.Effect)
	switch {
	case !ok:
		return fmt.Errorf("unknown effect %d", l.Effect)
	case l.Brightness < 0 || l.Brightness > MaxLevel || l.Speed < 0 || l.Speed > MaxLevel:
		return fmt.Errorf("brightness and speed must be 0–%d", MaxLevel)
	case !l.Colorful && !e.Color && l.Effect != EffectOff && l.Effect != EffectCustom:
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
	if _, ok := d.Effect(effect); !ok {
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
	if err := d.Check(l); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	cfg, err := d.readConfig()
	if err != nil {
		return err
	}
	if err := d.port.set(encode(cfg, l)); err != nil {
		return err
	}
	// The driver follows the config with the per-key colors for the custom
	// effect, and with the palette for the others.
	if l.Effect == EffectCustom {
		return d.port.set(keyReport(cmdKeys, &d.keyColors))
	}
	colors := d.colors
	if !l.Colorful && l.Effect != EffectOff {
		colors[l.Effect] = l.Color
	}
	if err := d.port.set(palette(&colors)); err != nil {
		return err
	}
	d.colors = colors
	return nil
}

// ---------- per-key colors ----------

// Keys is the number of matrix positions; not all have a key.
const Keys = 126

// KeyColors are colors by matrix index.
type KeyColors [Keys]razer.RGB

// The custom effect's colors are red, green and blue planes, one byte per
// matrix index; a frame has the three bytes of each index together.
const offRed, offGreen, offBlue = 8, 8 + Keys, 8 + 2*Keys

func keyReport(cmd byte, c *KeyColors) []byte {
	w := header(cmd, 0x7a, 1)
	for i, rgb := range c {
		if cmd == cmdFrame {
			w[8+3*i], w[9+3*i], w[10+3*i] = rgb.R, rgb.G, rgb.B
		} else {
			w[offRed+i], w[offGreen+i], w[offBlue+i] = rgb.R, rgb.G, rgb.B
		}
	}
	return w
}

// KeyColors returns the custom effect's colors the keyboard has.
func (d *Device) KeyColors() KeyColors {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.keyColors
}

// SetKeyColors tells the device the custom effect's colors, which it cannot
// report; ShowKeyColors also shows them.
func (d *Device) SetKeyColors(c KeyColors) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.keyColors = c
}

// ShowKeyColors switches to the custom effect with these colors. Already
// showing it, only the colors are written: every write goes to the
// keyboard's flash, and a burst of them makes it drop off USB.
func (d *Device) ShowKeyColors(c KeyColors) error {
	d.SetKeyColors(c)
	d.mu.Lock()
	cfg, err := d.readConfig()
	if err == nil && cfg[offEffect] == EffectCustom && cfg[offCustom] == 1 {
		err = d.port.set(keyReport(cmdKeys, &d.keyColors))
		d.mu.Unlock()
		return err
	}
	d.mu.Unlock()
	if err != nil {
		return err
	}
	return d.SetLighting(Lighting{Effect: EffectCustom})
}

// Frame shows colors until the next frame, without storing them.
func (d *Device) Frame(c *KeyColors) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.port.set(keyReport(cmdFrame, c))
}

// ---------- remaps ----------

// Layers: the default layer, and the one used while Fn is held.
const Layers = 2

// Action is what a key sends, as the firmware stores it.
type Action struct {
	Type int `json:"type"` // ActionKey, ActionMedia, or a keyboard function
	Mods int `json:"mods"` // ActionKey: modifier bits, left Ctrl 1, Shift 2, Alt 4, Win 8
	Code int `json:"code"` // ActionKey: HID keyboard usage, 0 for modifiers only; ActionMedia: consumer usage
}

// Action types; the Fn layer also holds keyboard functions (7: system, 8: lighting).
const (
	ActionKey   = 0
	ActionMedia = 2
)

// Layer is the actions by matrix index.
type Layer [Keys]Action

func (a Action) bytes() [4]byte {
	return [4]byte{byte(a.Type), byte(a.Mods), byte(a.Code >> 8), byte(a.Code)}
}

func actionOf(b []byte) Action {
	return Action{Type: int(b[0]), Mods: int(b[1]), Code: int(b[2])<<8 | int(b[3])}
}

// DefaultLayers are the remaps the model ships with.
func (m *Model) DefaultLayers() [Layers]Layer {
	var out [Layers]Layer
	for l, s := range m.defaultLayers {
		b, _ := hex.DecodeString(s)
		for i := range Keys {
			out[l][i] = actionOf(b[4*i:])
		}
	}
	return out
}

func keymapReport(layer int, l *Layer) []byte {
	w := header(cmdKeymap, 0, 2)
	w[2] = byte(layer)
	for i, a := range l {
		b := a.bytes()
		copy(w[8+4*i:], b[:])
	}
	copy(w[reportLen-2:], magic)
	return w
}

// FactoryKeyColors are the custom effect's colors after a factory reset.
func (m *Model) FactoryKeyColors() KeyColors {
	var c KeyColors
	for _, k := range m.factoryKeys {
		c[k] = razer.RGB{R: 0xff, G: 0xff, B: 0xff}
	}
	return c
}

// storedLayers: the firmware keeps 4 layers; the driver edits the first two
// and the third (Fn2, entered some way not known here), and resets all four.
const storedLayers = 4

// resetGap is the pause between a reset's writes, about the driver's: each
// goes to flash, and back to back they once garbled the config block and
// left every key dead. A write the keyboard refuses (still storing the one
// before, "device not functioning") is sent again resetTries times; every
// report is a whole block, so sending it twice is harmless. After the last,
// the keyboard is left alone for resetSettle to finish storing.
var (
	resetGap    = 100 * time.Millisecond
	resetTries  = 5
	resetSettle = 2 * time.Second
)

// Reset writes what the driver's factory reset writes, in its order: every
// layer's factory keys, the factory config, palette and per-key colors. The
// whole config block is written, not read and changed: a garbled block
// (keys dead) is then repaired too.
func (d *Device) Reset() error {
	cfg, err := hex.DecodeString(d.factoryConfig)
	if err != nil || len(cfg) != offMagic-8 {
		return errors.New("no factory settings known for this keyboard")
	}
	w := header(cmdConfig, configLen, 0)
	copy(w[8:], cfg)
	copy(w[offMagic:], magic)
	def := d.DefaultLayers()
	colors, keys := DefaultColors(), d.FactoryKeyColors()
	reports := [][]byte{}
	for i := range storedLayers {
		var l Layer
		if i < Layers {
			l = def[i]
		}
		reports = append(reports, keymapReport(i, &l))
	}
	reports = append(reports, w, palette(&colors), keyReport(cmdKeys, &keys))
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, r := range reports {
		if i > 0 {
			time.Sleep(resetGap)
		}
		for try := 1; ; try++ {
			err := d.port.set(r)
			if err == nil {
				break
			}
			if try == resetTries {
				return err
			}
			time.Sleep(3 * resetGap)
		}
	}
	d.colors, d.keyColors = colors, keys
	time.Sleep(resetSettle) // d.mu held: nothing reads or writes meanwhile
	return nil
}

// SetKeymap writes both layers, as the driver does after any change.
func (d *Device) SetKeymap(layers [Layers]Layer) error {
	if layers[0][d.FnIndex] != d.DefaultLayers()[0][d.FnIndex] {
		return errors.New("the Fn key cannot be remapped")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range layers {
		if err := d.port.set(keymapReport(i, &layers[i])); err != nil {
			return err
		}
	}
	return nil
}

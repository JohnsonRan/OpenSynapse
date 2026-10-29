// Package via controls keyboards running QMK with VIA enabled (Keychron and
// hundreds of others): their keymap and lighting, over VIA's raw HID
// protocol. The protocol is QMK's (quantum/via.h, GPL-2.0); the keyboards
// come from VIA's definitions (see catalog.go). None has been tried yet.
//
// Requests and answers are 32-byte reports without a report ID on the
// collection with usage page 0xFF60, usage 0x61: [command, data...]. The
// answer echoes the request with the values filled in; 0xFF means the
// firmware does not know the command.
package via

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ReportLen is the size of a request or an answer.
const ReportLen = 32

const (
	cmdProtocol    = 0x01
	cmdGetKeycode  = 0x04 // layer, row, col → keycode
	cmdSetKeycode  = 0x05 // layer, row, col, keycode
	cmdResetKeymap = 0x06 // back to the firmware's keymap
	cmdSetValue    = 0x07 // channel, value ID, data
	cmdGetValue    = 0x08
	cmdSaveValues  = 0x09 // channel
	cmdLayerCount  = 0x11
	cmdKeymapRead  = 0x12 // offset (2 bytes), size: the keymap as big-endian keycodes
	cmdUnhandled   = 0xFF
)

// MinProtocol is the oldest VIA protocol with the commands used here;
// lighting by channel needs ProtocolV3.
const (
	MinProtocol = 9
	ProtocolV3  = 12 // QMK 0.19: lighting by channel, and new keycode numbers
)

// Model is one keyboard of the definitions.
type Model struct {
	Name       string
	VID, PID   uint16
	Rows, Cols int
	Keys       []Key // at the layout options VIA picks by default
	Width      int   // of the drawing Keys are placed on
	Height     int
	Light      *Lighting // nil without lighting controls
}

// Key is one key on the drawing and its place in the switch matrix.
type Key struct {
	Row, Col   int
	X, Y, W, H int
}

// Index is the key's place in a layer's keymap.
func (m *Model) Index(row, col int) int { return row*m.Cols + col }

// Value is where a lighting setting is: its channel and ID, and the largest
// value of a range.
type Value struct {
	Channel, ID byte
	Max         int
}

// Lighting are a keyboard's lighting controls; any may be missing.
type Lighting struct {
	Brightness, Effect, Speed, Color *Value
	Effects                          []Effect // the effect values and their names
}

// Effect is one value of the effect control.
type Effect struct {
	Value int
	Name  string
}

// State is what the keyboard's lighting is set to. Brightness and Speed go
// up to their Value's Max; Hue and Sat are 0–255.
type State struct {
	Effect, Brightness, Speed int
	Hue, Sat                  byte
}

// port carries reports; tests replace it.
type port interface {
	send(report []byte) error
	replies() <-chan []byte
	close()
}

// Device is a connected keyboard.
type Device struct {
	*Model
	Variants []*Model // the models sharing its USB IDs, Model among them
	ID       string   // USB IDs, e.g. 3434:0100
	Protocol int
	Layers   int
	mu       sync.Mutex
	port     port
}

var replyTimeout = time.Second

var errUnhandled = errors.New("the keyboard does not know this request")

// request sends a request and waits for its answer.
func (d *Device) request(data ...byte) ([]byte, error) {
	r := make([]byte, ReportLen)
	copy(r, data)
	// Late answers to an earlier request that timed out would be taken for this one.
	for drained := false; !drained; {
		select {
		case <-d.port.replies():
		default:
			drained = true
		}
	}
	if err := d.port.send(r); err != nil {
		return nil, err
	}
	timeout := time.After(replyTimeout)
	for {
		select {
		case a, ok := <-d.port.replies():
			if !ok {
				return nil, errors.New("keyboard disconnected")
			}
			if len(a) < ReportLen {
				continue
			}
			if a[0] == cmdUnhandled {
				return nil, errUnhandled
			}
			if a[0] == data[0] {
				return a[:ReportLen], nil
			}
		case <-timeout:
			return nil, errors.New("keyboard did not answer")
		}
	}
}

// open asks the keyboard for its protocol and number of layers.
func (d *Device) open() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	a, err := d.request(cmdProtocol)
	if err != nil {
		return err
	}
	d.Protocol = int(binary.BigEndian.Uint16(a[1:]))
	if d.Protocol < MinProtocol {
		return fmt.Errorf("VIA protocol %d is too old", d.Protocol)
	}
	a, err = d.request(cmdLayerCount)
	if err != nil {
		return err
	}
	if d.Layers = int(a[1]); d.Layers < 1 {
		return errors.New("no keymap layers")
	}
	return nil
}

// Keymap reads a layer's keycodes, by Index.
func (d *Device) Keymap(layer int) ([]uint16, error) {
	if layer < 0 || layer >= d.Layers {
		return nil, fmt.Errorf("no layer %d", layer)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	n := d.Rows * d.Cols
	buf := make([]byte, 0, 2*n)
	const chunk = ReportLen - 4 // as VIA reads it
	for off := layer * n * 2; len(buf) < 2*n; off += chunk {
		size := min(chunk, 2*n-len(buf))
		a, err := d.request(cmdKeymapRead, byte(off>>8), byte(off), byte(size))
		if err != nil {
			return nil, err
		}
		buf = append(buf, a[4:4+size]...)
	}
	out := make([]uint16, n)
	for i := range out {
		out[i] = binary.BigEndian.Uint16(buf[2*i:])
	}
	return out, nil
}

// Keycode reads one key of a layer.
func (d *Device) Keycode(layer, row, col int) (uint16, error) {
	if layer < 0 || layer >= d.Layers || row < 0 || row >= d.Rows || col < 0 || col >= d.Cols {
		return 0, errors.New("no such key")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	a, err := d.request(cmdGetKeycode, byte(layer), byte(row), byte(col))
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(a[4:]), nil
}

// SetKeycode sets one key of a layer; the firmware stores it at once.
func (d *Device) SetKeycode(layer, row, col int, code uint16) error {
	if layer < 0 || layer >= d.Layers || row < 0 || row >= d.Rows || col < 0 || col >= d.Cols {
		return errors.New("no such key")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	a, err := d.request(cmdSetKeycode, byte(layer), byte(row), byte(col), byte(code>>8), byte(code))
	if err == nil && binary.BigEndian.Uint16(a[4:]) != code {
		err = errors.New("keyboard kept another keycode")
	}
	return err
}

// ResetKeymap gives every key the firmware's keymap back.
func (d *Device) ResetKeymap() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.request(cmdResetKeymap)
	return err
}

// Use makes the keyboard one of its variants, by name.
func (d *Device) Use(name string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, m := range d.Variants {
		if m.Name == name {
			d.Model = m
			return true
		}
	}
	return false
}

// HasLighting reports whether the keyboard's lighting can be changed here.
func (d *Device) HasLighting() bool { return d.Light != nil && d.Protocol >= ProtocolV3 }

func (d *Device) get(v *Value, n int) ([]byte, error) {
	a, err := d.request(cmdGetValue, v.Channel, v.ID)
	if err != nil {
		return nil, err
	}
	return a[3 : 3+n], nil
}

// Lighting reads the lighting settings.
func (d *Device) Lighting() (State, error) {
	if !d.HasLighting() {
		return State{}, errors.New("no lighting controls")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var s State
	for _, f := range []struct {
		v   *Value
		dst *int
	}{{d.Light.Brightness, &s.Brightness}, {d.Light.Effect, &s.Effect}, {d.Light.Speed, &s.Speed}} {
		if f.v == nil {
			continue
		}
		b, err := d.get(f.v, 1)
		if err != nil {
			return State{}, err
		}
		*f.dst = int(b[0])
	}
	if v := d.Light.Color; v != nil {
		b, err := d.get(v, 2)
		if err != nil {
			return State{}, err
		}
		s.Hue, s.Sat = b[0], b[1]
	}
	return s, nil
}

// SetLighting sets the lighting and stores it.
func (d *Device) SetLighting(s State) error {
	if !d.HasLighting() {
		return errors.New("no lighting controls")
	}
	l := d.Light
	if l.Effect != nil && !l.hasEffect(s.Effect) {
		return fmt.Errorf("unknown effect %d", s.Effect)
	}
	for _, f := range []struct {
		v   *Value
		val int
	}{{l.Brightness, s.Brightness}, {l.Speed, s.Speed}} {
		if f.v != nil && (f.val < 0 || f.val > f.v.Max) {
			return fmt.Errorf("value %d out of range", f.val)
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	channels := map[byte]bool{}
	set := func(v *Value, data ...byte) error {
		if v == nil {
			return nil
		}
		channels[v.Channel] = true
		_, err := d.request(append([]byte{cmdSetValue, v.Channel, v.ID}, data...)...)
		return err
	}
	for _, err := range []error{
		set(l.Effect, byte(s.Effect)),
		set(l.Brightness, byte(s.Brightness)),
		set(l.Speed, byte(s.Speed)),
		set(l.Color, s.Hue, s.Sat),
	} {
		if err != nil {
			return err
		}
	}
	for ch := range channels {
		if _, err := d.request(cmdSaveValues, ch); err != nil {
			return err
		}
	}
	return nil
}

func (l *Lighting) hasEffect(v int) bool {
	for _, e := range l.Effects {
		if e.Value == v {
			return true
		}
	}
	return false
}

// Close releases the keyboard.
func (d *Device) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.port.close()
}

// Package evision controls the lighting of keyboards on EVision firmware
// (rebranded Sonix chips in Redragon, AJAZZ, VGN, MageGee and many other
// keyboards). The protocol follows OpenRGB's EVision and EVision V2
// controllers (GPL-2.0-or-later); no code is copied. None has been tried.
//
// Every request and answer is a 64-byte report, ID 4, on the collection with
// usage page 0xFF1C: [4, checksum (2 bytes, sum of bytes 3–63), cmd, size,
// offset (2 bytes), status, data...]. V2 firmware answers reads and has
// three profiles of settings; V1 is only written to, at the offset of each
// setting, so the app remembers what it set.
package evision

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	reportLen = 64
	reportID  = 4
	dataLen   = reportLen - 8

	cmdBegin       = 0x01
	cmdEnd         = 0x02
	cmdCapabilites = 0x03
	cmdReadConfig  = 0x05
	cmdWriteConfig = 0x06
)

// Settings of the keyboard zone, by offset in a profile (V2) or as V1's
// parameter number.
const (
	parMode       = 0x00
	parBrightness = 0x01
	parSpeed      = 0x02
	parDirection  = 0x03
	parRandom     = 0x04 // nonzero: the effect cycles colors
	parColor      = 0x05 // red, green, blue
	parColorV2    = 0x08 // V2: an offset into the colors, written 0
	parLEDMode    = 0x11
	zoneLen       = 18 // V2: the keyboard zone's settings, written whole

	profileLen   = 0x40
	firstProfile = 0x01 // config offset of profile 0; offset 0 is the current profile
)

// MaxBrightness and MaxSpeed are the firmware's scales; speed 0 is the fastest.
const (
	MaxBrightness = 4
	MaxSpeed      = 5
)

// Effect is one of the firmware's effects.
type Effect struct {
	ID   int
	Name string // the app's translation key (kb.effect.*)
}

// Effects are the effects of both firmware versions, by ID.
var Effects = []Effect{
	{0x06, "static"}, {0x05, "breathing"}, {0x04, "rainbow"}, {0x01, "waveShort"}, {0x02, "waveLong"},
	{0x03, "colorWheel"}, {0x07, "reactive"}, {0x08, "ripples"}, {0x09, "reactiveLine"}, {0x0A, "stars"},
	{0x0B, "blossom"}, {0x0C, "rainbowWave"}, {0x0D, "hurricane"}, {0x0E, "accumulate"}, {0x0F, "starlightSlow"},
	{0x10, "visor"}, {0x11, "surmount"}, {0x12, "rainbowCircle"},
}

// Lighting is what the keyboard zone shows.
type Lighting struct {
	Effect     int
	Brightness int // 0–MaxBrightness; 0 is dark
	Speed      int // 0 (fastest) to MaxSpeed
	Random     bool
	R, G, B    byte
}

// port carries reports; tests replace it.
type port interface {
	send(report []byte) error
	replies() <-chan []byte
	close()
}

// Device is a connected keyboard.
type Device struct {
	ID, Name string
	V2       bool // answers reads
	mu       sync.Mutex
	port     port
	last     *Lighting // V1: what was set last, nil until then
}

var replyTimeout = time.Second

func request(cmd byte, offset uint16, data []byte, size byte) []byte {
	r := make([]byte, reportLen)
	r[0], r[3], r[4], r[5], r[6] = reportID, cmd, size, byte(offset), byte(offset>>8)
	copy(r[8:], data)
	var sum uint16
	for _, b := range r[3:] {
		sum += uint16(b)
	}
	r[1], r[2] = byte(sum), byte(sum>>8)
	return r
}

// query sends one request and returns the data of its answer, which echoes
// the request's checksum, command and offset.
func (d *Device) query(cmd byte, offset uint16, data []byte, size byte) ([]byte, error) {
	for drained := false; !drained; {
		select {
		case _, ok := <-d.port.replies():
			if !ok {
				return nil, errors.New("keyboard disconnected")
			}
		default:
			drained = true
		}
	}
	r := request(cmd, offset, data, size)
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
			if len(a) != reportLen || a[0] != reportID {
				continue
			}
			if !d.V2 {
				return nil, nil // V1's answers are not documented; OpenRGB only waits for one
			}
			if a[1] != r[1] || a[2] != r[2] || a[3] != cmd || a[5] != r[5] || a[6] != r[6] {
				continue
			}
			if a[7] != 0 {
				return nil, fmt.Errorf("keyboard refused the request (%d)", a[7])
			}
			if a[4] > dataLen {
				return nil, errors.New("answer too long")
			}
			return a[8 : 8+a[4]], nil
		case <-timeout:
			return nil, errors.New("keyboard did not answer")
		}
	}
}

func (d *Device) read(cmd byte, offset uint16, size int) ([]byte, error) {
	var out []byte
	for len(out) < size {
		b, err := d.query(cmd, offset+uint16(len(out)), nil, byte(min(size-len(out), dataLen)))
		if err != nil {
			return nil, err
		}
		if len(b) == 0 {
			return nil, errors.New("empty answer")
		}
		out = append(out, b...)
	}
	return out[:size], nil
}

func (d *Device) write(cmd byte, offset uint16, data []byte) error {
	for off := 0; off < len(data); off += dataLen {
		chunk := data[off:min(len(data), off+dataLen)]
		if _, err := d.query(cmd, offset+uint16(off), chunk, byte(len(chunk))); err != nil {
			return err
		}
	}
	return nil
}

// identify checks for V2 firmware: its capabilities start with AA 55.
func (d *Device) identify() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.V2 = true // only V2 answers are checked
	b, err := d.read(cmdCapabilites, 0, 7)
	d.V2 = err == nil && b[0] == 0xAA && b[1] == 0x55
	return d.V2
}

// profile is the config offset of the current profile's settings.
func (d *Device) profile() (uint16, error) {
	b, err := d.read(cmdReadConfig, 0, 1)
	if err != nil {
		return 0, err
	}
	p := b[0]
	if p > 2 {
		p = 0
	}
	return firstProfile + uint16(p)*profileLen, nil
}

// Lighting reads what the keyboard zone shows; V1 reports what was set last.
func (d *Device) Lighting() (Lighting, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.V2 {
		if d.last == nil {
			return Lighting{Effect: 0x06, Brightness: MaxBrightness, Speed: 3, R: 0xff, G: 0xff, B: 0xff}, nil
		}
		return *d.last, nil
	}
	at, err := d.profile()
	if err != nil {
		return Lighting{}, err
	}
	b, err := d.read(cmdReadConfig, at, zoneLen)
	if err != nil {
		return Lighting{}, err
	}
	return Lighting{Effect: int(b[parMode]), Brightness: int(b[parBrightness]), Speed: int(b[parSpeed]), Random: b[parRandom] != 0,
		R: b[parColor], G: b[parColor+1], B: b[parColor+2]}, nil
}

// SetLighting sets and stores the keyboard zone's lighting.
func (d *Device) SetLighting(l Lighting) error {
	if !hasEffect(l.Effect) || l.Brightness < 0 || l.Brightness > MaxBrightness || l.Speed < 0 || l.Speed > MaxSpeed {
		return fmt.Errorf("lighting %+v out of range", l)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	random := byte(0)
	if l.Random {
		random = 0xff // as OpenRGB writes V2; V1 takes 1
		if !d.V2 {
			random = 1
		}
	}
	if !d.V2 {
		// V1 takes the mode and its settings in one write at parameter 0, as OpenRGB sends it.
		if err := d.write(cmdWriteConfig, parMode, []byte{byte(l.Effect), byte(l.Brightness), byte(l.Speed), 0, random, l.R, l.G, l.B}); err != nil {
			return err
		}
		d.last = &l
		return nil
	}
	at, err := d.profile()
	if err != nil {
		return err
	}
	cur, err := d.read(cmdReadConfig, at, zoneLen)
	if err != nil {
		return err
	}
	z := append([]byte(nil), cur...) // the direction and LED mode stay as read
	z[parMode], z[parBrightness], z[parSpeed], z[parRandom] = byte(l.Effect), byte(l.Brightness), byte(l.Speed), random
	z[parColor], z[parColor+1], z[parColor+2], z[parColorV2] = l.R, l.G, l.B, 0
	if _, err := d.query(cmdBegin, 0, nil, 0); err != nil {
		return err
	}
	err = d.write(cmdWriteConfig, at, z)
	if _, e := d.query(cmdEnd, 0, nil, 0); err == nil {
		err = e
	}
	return err
}

func hasEffect(id int) bool {
	for _, e := range Effects {
		if e.ID == id {
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

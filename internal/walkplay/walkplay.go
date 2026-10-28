// Package walkplay controls USB audio dongles built on WalkPlay chips: their
// parametric EQ and DAC options. The protocol follows the WalkPlay web app as
// documented by devicePEQ (https://github.com/jeromeof/devicePEQ, ISC
// license); no code is copied. Only the EPZ TP35 Pro has been tried.
//
// Requests and replies are 64-byte HID reports, ID 0x4B:
// [0x4B, 0x80 read | 0x01 write, command, data...].
package walkplay

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

const (
	reportID = 0x4B
	opRead   = 0x80
	opWrite  = 0x01
)

// Commands.
const (
	cmdFlashEQ   = 0x01
	cmdMicGain   = 0x02
	cmdPreamp    = 0x03
	cmdBand      = 0x09
	cmdTempWrite = 0x0A
	cmdVersion   = 0x0C
	cmdDACFilter = 0x11
	cmdGainMode  = 0x19
	cmdWorkMode  = 0x1D
)

// Bands is how many EQ bands the supported dongles have.
const Bands = 10

// Band is one EQ filter. Type is "peak", "lowShelf" or "highShelf".
type Band struct {
	Freq int     `json:"freq"` // Hz
	Q    float64 `json:"q"`
	Gain float64 `json:"gain"` // dB
	Type string  `json:"type"`
}

// EQ is the dongle's equalizer: its bands and the gain applied before them.
type EQ struct {
	Bands  []Band `json:"bands"`
	Preamp int    `json:"preamp"` // dB
}

var filterTypes = map[string]byte{"lowShelf": 1, "peak": 2, "highShelf": 3}

func typeName(b byte) string {
	for n, v := range filterTypes {
		if v == b {
			return n
		}
	}
	return "peak"
}

// Check reports whether the dongle accepts the EQ.
func (e EQ) Check() error {
	if len(e.Bands) != Bands {
		return fmt.Errorf("need %d bands", Bands)
	}
	if e.Preamp < -10 || e.Preamp > 10 {
		return errors.New("preamp must be -10 to 10 dB")
	}
	for _, b := range e.Bands {
		if _, ok := filterTypes[b.Type]; !ok || b.Freq < 20 || b.Freq > 20000 || b.Q < 0.1 || b.Q > 10 || b.Gain < -10 || b.Gain > 10 {
			return errors.New("band out of range: 20–20000 Hz, Q 0.1–10, -10 to 10 dB")
		}
	}
	return nil
}

// port carries reports to and from the dongle; tests replace it.
type port interface {
	send(report []byte) error
	replies() <-chan []byte
}

// Device is a connected dongle.
type Device struct {
	ID, Name string
	USB      string // VID_xxxx&PID_xxxx, to find its sound device
	mu       sync.Mutex
	port     port
	slot     byte // the EQ preset the dongle uses, learned when reading the EQ
}

// request sends a read and returns the reply that starts like it. For about
// a second after saving the EQ the dongle ignores requests, so it asks again.
func (d *Device) request(want ...byte) ([]byte, error) {
	for range 6 {
	drain: // drop replies nobody waited for
		for {
			select {
			case _, ok := <-d.port.replies():
				if !ok {
					break drain
				}
			default:
				break drain
			}
		}
		if err := d.write(want...); err != nil {
			return nil, err
		}
		if r, err := d.reply(want); r != nil || err != nil {
			return r, err
		}
	}
	return nil, errors.New("device did not answer")
}

// reply waits briefly for the answer to want; nil, nil when none came.
func (d *Device) reply(want []byte) ([]byte, error) {
	t := time.NewTimer(400 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case r, ok := <-d.port.replies():
			if !ok {
				return nil, errors.New("device disconnected")
			}
			if len(r) > len(want) && r[0] == reportID && string(r[1:1+len(want)]) == string(want) {
				return r, nil
			}
		case <-t.C:
			return nil, nil
		}
	}
}

// write sends one report without waiting for a reply.
func (d *Device) write(data ...byte) error {
	r := make([]byte, 64)
	r[0] = reportID
	copy(r[1:], data)
	return d.port.send(r)
}

func (d *Device) readByte(cmd byte) (byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, err := d.request(opRead, cmd)
	if err != nil {
		return 0, err
	}
	return r[4], nil
}

func (d *Device) writeByte(cmd, v byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.write(opWrite, cmd, 0x01, v)
}

// Version returns the firmware version, e.g. "1.7".
func (d *Device) Version() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, err := d.request(opRead, cmdVersion)
	if err != nil {
		return "", err
	}
	return string(r[4:7]), nil
}

// EQ reads the bands and the preamp.
func (d *Device) EQ() (EQ, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e := EQ{Bands: make([]Band, Bands)}
	for i := range Bands {
		r, err := d.request(opRead, cmdBand, 0, 0, byte(i))
		if err != nil {
			return EQ{}, err
		}
		e.Bands[i], d.slot = decodeBand(r)
	}
	r, err := d.request(opRead, cmdPreamp)
	if err != nil {
		return EQ{}, err
	}
	e.Preamp = int(int8(r[5]))
	return e, nil
}

// SetEQ writes the bands and the preamp, and saves them in the dongle.
func (d *Device) SetEQ(e EQ) error {
	if err := e.Check(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, b := range e.Bands {
		if err := d.write(encodeBand(i, b, d.slot)...); err != nil {
			return err
		}
		time.Sleep(20 * time.Millisecond) // the WalkPlay app paces its writes
	}
	// The WalkPlay app's commit sequence: preamp, then apply and save.
	for _, r := range [][]byte{
		{opWrite, cmdPreamp, 0x02, 0x00, byte(int8(e.Preamp))},
		{opWrite, 0x05, 0x00},
		{opWrite, 0x17, 0x00},
		{opWrite, cmdTempWrite, 0x04, 0x00, 0x00, 0xFF, 0xFF, 0x00},
		{opWrite, cmdFlashEQ, 0x01, 0x00},
	} {
		time.Sleep(20 * time.Millisecond)
		if err := d.write(r...); err != nil {
			return err
		}
	}
	return nil
}

// DACFilter is the DAC's digital filter: 1 fast low-latency, 2 fast linear
// phase, 3 slow low-latency, 4 slow linear phase, 5 non-oversampling.
func (d *Device) DACFilter() (int, error) {
	v, err := d.readByte(cmdDACFilter)
	return int(v), err
}

func (d *Device) SetDACFilter(f int) error {
	if f < 1 || f > 5 {
		return errors.New("DAC filter must be 1–5")
	}
	return d.writeByte(cmdDACFilter, byte(f))
}

// HighGain reports whether the amplifier runs in high gain.
func (d *Device) HighGain() (bool, error) {
	v, err := d.readByte(cmdGainMode)
	return v == 1, err
}

func (d *Device) SetHighGain(on bool) error {
	return d.writeByte(cmdGainMode, map[bool]byte{false: 0, true: 1}[on])
}

// WorkMode is the amplifier class: 0 class H (saves power), 1 class AB;
// checked against the WalkPlay app on a TP35 Pro.
func (d *Device) WorkMode() (int, error) {
	v, err := d.readByte(cmdWorkMode)
	return int(v), err
}

func (d *Device) SetWorkMode(m int) error {
	if m != 0 && m != 1 {
		return errors.New("work mode must be 0 or 1")
	}
	return d.writeByte(cmdWorkMode, byte(m))
}

// MicGain is the microphone gain in dB, -15 to 15.
func (d *Device) MicGain() (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, err := d.request(opRead, cmdMicGain)
	if err != nil {
		return 0, err
	}
	return int(math.Round(float64(int16(uint16(r[4])|uint16(r[5])<<8)) * 15 / 32767)), nil
}

func (d *Device) SetMicGain(db int) error {
	if db < -15 || db > 15 {
		return errors.New("mic gain must be -15 to 15 dB")
	}
	v := uint16(int16(math.Round(float64(db) * 32767 / 15)))
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.write(opWrite, cmdMicGain, 0x02, byte(v), byte(v>>8))
}

// encodeBand is the write of band i: the biquad coefficients the dongle
// filters with, then the settings the app reads back.
func encodeBand(i int, b Band, slot byte) []byte {
	out := []byte{opWrite, cmdBand, 0x18, 0x00, byte(i), 0x00, 0x00}
	out = append(out, biquad(b)...)
	q, g := uint16(math.Round(b.Q*256)), uint16(int16(math.Round(b.Gain*256)))
	return append(out, byte(b.Freq), byte(b.Freq>>8), byte(q), byte(q>>8), byte(g), byte(g>>8), filterTypes[b.Type], 0x00, slot, 0x00)
}

// decodeBand reads a band reply (with its report ID); it also carries the preset slot.
func decodeBand(r []byte) (Band, byte) {
	return Band{
		Freq: int(r[28]) | int(r[29])<<8,
		Q:    math.Round(float64(uint16(r[30])|uint16(r[31])<<8)/256*100) / 100,
		Gain: math.Round(float64(int16(uint16(r[32])|uint16(r[33])<<8))/256*100) / 100,
		Type: typeName(r[34]),
	}, r[36]
}

// biquad returns the RBJ cookbook coefficients b0 b1 b2 -a1 -a2, normalized
// and in Q2.30, little-endian; the dongle designs them for 96 kHz.
func biquad(b Band) []byte {
	A := math.Pow(10, b.Gain/40)
	w0 := 2 * math.Pi * float64(b.Freq) / 96000
	sin, cos := math.Sin(w0), math.Cos(w0)
	var b0, b1, b2, a0, a1, a2 float64
	switch b.Type {
	case "lowShelf", "highShelf":
		s := 2 * math.Sqrt(A) * sin / 2 * math.Sqrt((A+1/A)*(1/b.Q-1)+2)
		sign := 1.0
		if b.Type == "highShelf" {
			sign = -1
		}
		b0 = A * ((A + 1) - sign*(A-1)*cos + s)
		b1 = sign * 2 * A * ((A - 1) - sign*(A+1)*cos)
		b2 = A * ((A + 1) - sign*(A-1)*cos - s)
		a0 = (A + 1) + sign*(A-1)*cos + s
		a1 = -sign * 2 * ((A - 1) + sign*(A+1)*cos)
		a2 = (A + 1) + sign*(A-1)*cos - s
	default:
		alpha := sin / (2 * b.Q)
		b0, b1, b2 = 1+alpha*A, -2*cos, 1-alpha*A
		a0, a1, a2 = 1+alpha/A, -2*cos, 1-alpha/A
	}
	out := make([]byte, 0, 20)
	for _, c := range []float64{b0 / a0, b1 / a0, b2 / a0, -a1 / a0, -a2 / a0} {
		v := uint32(int32(math.Round(c * (1 << 30))))
		out = append(out, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
	return out
}

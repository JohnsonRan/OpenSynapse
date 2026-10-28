// Package winaudio reads and sets the format Windows plays to a sound device
// in shared mode: what Sound settings > device properties > Advanced calls
// the default format (bit depth and sample rate).
package winaudio

import (
	"encoding/binary"
	"errors"
	"slices"

	"golang.org/x/sys/windows"
)

// Format is a bit depth and a sample rate.
type Format struct {
	Bits int `json:"bits"`
	Rate int `json:"rate"`
}

// Candidates Windows offers; a device supports some of them.
var (
	candidateBits  = []int{16, 24, 32}
	candidateRates = []int{44100, 48000, 88200, 96000, 176400, 192000, 352800, 384000}
)

// containers are the sample sizes a bit depth can travel in, as drivers expect them.
func containers(bits int) []int {
	if bits == 24 {
		return []int{24, 32}
	}
	return []int{bits}
}

var (
	subtypePCM   = windows.GUID{Data1: 0x00000001, Data2: 0x0000, Data3: 0x0010, Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	subtypeFloat = windows.GUID{Data1: 0x00000003, Data2: 0x0000, Data3: 0x0010, Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
)

const formatExtensible = 0xFFFE

// waveFormat is a WAVEFORMATEXTENSIBLE: container bits per sample, valid bits.
func waveFormat(channels, rate, container, valid int, mask uint32, sub windows.GUID) []byte {
	b := make([]byte, 40)
	le := binary.LittleEndian
	align := channels * container / 8
	le.PutUint16(b[0:], formatExtensible)
	le.PutUint16(b[2:], uint16(channels))
	le.PutUint32(b[4:], uint32(rate))
	le.PutUint32(b[8:], uint32(rate*align))
	le.PutUint16(b[12:], uint16(align))
	le.PutUint16(b[14:], uint16(container))
	le.PutUint16(b[16:], 22) // cbSize: the extensible part
	le.PutUint16(b[18:], uint16(valid))
	le.PutUint32(b[20:], mask)
	le.PutUint32(b[24:], sub.Data1)
	le.PutUint16(b[28:], sub.Data2)
	le.PutUint16(b[30:], sub.Data3)
	copy(b[32:], sub.Data4[:])
	return b
}

// parsed is what matters of a WAVEFORMATEX(TENSIBLE).
type parsed struct {
	channels int
	mask     uint32
	format   Format
}

func parseWaveFormat(b []byte) (parsed, error) {
	if len(b) < 16 {
		return parsed{}, errors.New("short wave format")
	}
	le := binary.LittleEndian
	p := parsed{channels: int(le.Uint16(b[2:])), format: Format{Bits: int(le.Uint16(b[14:])), Rate: int(le.Uint32(b[4:]))}}
	if le.Uint16(b[0:]) == formatExtensible && len(b) >= 40 {
		p.format.Bits = int(le.Uint16(b[18:]))
		p.mask = le.Uint32(b[20:])
	}
	if p.mask == 0 {
		p.mask = map[int]uint32{1: 0x4, 2: 0x3}[p.channels] // centre; front left and right
	}
	return p, nil
}

// supported returns the candidates the device takes, given whether a wave
// format (container, valid bits) at a rate is supported.
func supported(ok func(container, valid, rate int) bool) []Format {
	var out []Format
	for _, bits := range candidateBits {
		for _, rate := range candidateRates {
			if slices.ContainsFunc(containers(bits), func(c int) bool { return ok(c, bits, rate) }) {
				out = append(out, Format{bits, rate})
			}
		}
	}
	return out
}

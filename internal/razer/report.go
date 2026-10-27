// Package razer speaks the Razer feature-report protocol documented by
// openrazer (https://github.com/openrazer/openrazer). Reimplemented from the
// documented packet layout; no openrazer code is copied.
package razer

import (
	"errors"
	"fmt"
)

// ReportLen is a Windows HID feature report: report ID byte + 90-byte Razer report.
const ReportLen = 91

// Byte offsets inside a Windows feature report (index 0 is the HID report ID).
const (
	offStatus = 1
	offTxID   = 2
	offSize   = 6
	offClass  = 7
	offCmd    = 8
	offArgs   = 9
	offCRC    = 89
)

const (
	statusBusy        = 0x01
	statusOK          = 0x02
	statusFailure     = 0x03
	statusTimeout     = 0x04
	statusUnsupported = 0x05
)

var (
	ErrUnsupported = errors.New("device does not support this command")
	ErrNoAnswer    = errors.New("device did not answer (asleep or out of range?)")
)

// Report is one request or response.
type Report [ReportLen]byte

// NewReport builds a request with class/cmd, declared data size and arguments.
func NewReport(txID, class, cmd, size byte, args ...byte) Report {
	if len(args) > 80 || int(size) > 80 {
		panic("razer: report arguments exceed 80 bytes")
	}
	var r Report
	r[offTxID] = txID
	r[offSize] = size
	r[offClass] = class
	r[offCmd] = cmd
	copy(r[offArgs:], args)
	r[offCRC] = r.crc()
	return r
}

func (r *Report) crc() byte {
	var c byte
	for i := 3; i <= 88; i++ {
		c ^= r[i]
	}
	return c
}

// Args returns the 80 argument bytes.
func (r *Report) Args() []byte { return r[offArgs : offArgs+80] }

// check validates a response against its request.
func (r *Report) check(req *Report) error {
	if r[offTxID] != req[offTxID] || r[offClass] != req[offClass] || r[offCmd] != req[offCmd] || r[offCRC] != r.crc() {
		return fmt.Errorf("mismatched response (class %02x cmd %02x)", r[offClass], r[offCmd])
	}
	switch r[offStatus] {
	case statusOK, statusBusy:
		return nil
	case statusUnsupported:
		return ErrUnsupported
	case statusTimeout:
		return ErrNoAnswer
	default:
		return fmt.Errorf("device status 0x%02x", r[offStatus])
	}
}

func be16(b []byte) int { return int(b[0])<<8 | int(b[1]) }
func put16(v int) []byte { return []byte{byte(v >> 8), byte(v)} }

// Storage selector for DPI commands.
const (
	noStore  = 0x00
	varStore = 0x01
)

// Poll-rate codes for the "polling rate 2" command family (HyperPolling devices).
var pollCodes = map[int]byte{8000: 0x01, 4000: 0x02, 2000: 0x04, 1000: 0x08, 500: 0x10, 250: 0x20, 125: 0x40}

func pollFromCode(c byte) int {
	for hz, code := range pollCodes {
		if code == c {
			return hz
		}
	}
	return 0
}

// DPIStage is one sensitivity stage.
type DPIStage struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func encodeStages(stages []DPIStage, active int) []byte {
	args := []byte{varStore, byte(active), byte(len(stages))}
	for i, s := range stages {
		args = append(args, byte(i))
		args = append(args, put16(s.X)...)
		args = append(args, put16(s.Y)...)
		args = append(args, 0, 0)
	}
	return args
}

// decodeStages parses a get_dpi_stages response: varstore, active, count, then 7 bytes per stage.
func decodeStages(args []byte, size int) (stages []DPIStage, active int) {
	active, n := int(args[1]), int(args[2])
	for i, off := 0, 4; i < n && off+4 <= size; i, off = i+1, off+7 {
		stages = append(stages, DPIStage{X: be16(args[off:]), Y: be16(args[off+2:])})
	}
	return stages, active
}

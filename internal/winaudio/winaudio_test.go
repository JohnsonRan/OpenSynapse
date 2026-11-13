package winaudio

import (
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"testing"
)

func TestWaveFormatRoundTrip(t *testing.T) {
	b := waveFormat(2, 96000, 32, 24, 0x3, subtypePCM)
	if len(b) != 40 || b[12] != 8 || b[8] != 0x00 || b[9] != 0xB8 || b[10] != 0x0B { // align 8, 768000 bytes/s
		t.Fatalf("% X", b)
	}
	p, err := parseWaveFormat(b)
	if err != nil || p.channels != 2 || p.mask != 3 || p.format != (Format{24, 96000}) {
		t.Fatalf("%+v %v", p, err)
	}
	// A plain WAVEFORMATEX: the bits are the container's, the mask comes from the channels.
	p, _ = parseWaveFormat(b[:18])
	if p.format.Bits != 32 || p.mask != 3 {
		t.Fatalf("%+v", p)
	}
}

func TestSupportedTriesBothContainersFor24Bit(t *testing.T) {
	got := supported(func(container, valid, rate int) bool {
		return rate == 48000 && (valid == 16 || container == 32 && valid == 24)
	})
	if !slices.Equal(got, []Format{{16, 48000}, {24, 48000}}) {
		t.Fatalf("%v", got)
	}
}

func TestDefaultInvalidFlowReturnsError(t *testing.T) {
	if _, err := Default(Flow(99)); err == nil {
		t.Fatal("expected error for invalid flow")
	}
}

func TestSetVolumeRange(t *testing.T) {
	if SetVolume("x", 101) == nil || SetVolume("x", -1) == nil {
		t.Fatal("accepted a volume outside 0–100")
	}
}

func TestMixDown(t *testing.T) {
	// Stereo float: (0.5, -0.25) → 0.125; 16-bit: (16384, 16384) → 0.5; 24-bit full scale left only → ~0.5.
	f := make([]byte, 8)
	binary.LittleEndian.PutUint32(f, math.Float32bits(0.5))
	binary.LittleEndian.PutUint32(f[4:], math.Float32bits(-0.25))
	if got := mixDown(nil, f, 2, 8, true); len(got) != 1 || got[0] != 0.125 {
		t.Fatalf("float: %v", got)
	}
	if got := mixDown(nil, []byte{0, 0x40, 0, 0x40}, 2, 4, false); got[0] != 0.5 {
		t.Fatalf("16-bit: %v", got)
	}
	if got := mixDown(nil, []byte{0xff, 0xff, 0x7f, 0, 0, 0}, 2, 6, false); math.Abs(float64(got[0])-0.5) > 1e-6 {
		t.Fatalf("24-bit: %v", got)
	}
}

// A stream that fails must end the capture: taking the failure for "no
// packet waits" left Loopback polling a dead stream.
func TestDrainEndsOnAStreamError(t *testing.T) {
	invalidated := hrErr("GetNextPacketSize", 0x88890004) // AUDCLNT_E_DEVICE_INVALIDATED
	sizes := []uint32{480, 480, 0}
	reads := 0
	next := func() (uint32, error) { n := sizes[0]; sizes = sizes[1:]; return n, nil }
	if err := drain(next, func() error { reads++; return nil }); err != nil || reads != 2 {
		t.Fatalf("%d packets read, %v; want the two that waited", reads, err)
	}
	if err := drain(func() (uint32, error) { return 0, invalidated }, func() error { reads++; return nil }); err != invalidated || reads != 2 {
		t.Fatalf("after a failed stream: %v, %d reads; want its error and no read", err, reads)
	}
	failed := errors.New("GetBuffer")
	if err := drain(func() (uint32, error) { return 480, nil }, func() error { return failed }); err != failed {
		t.Fatalf("after a failed read: %v", err)
	}
}

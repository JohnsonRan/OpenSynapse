package winaudio

import (
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

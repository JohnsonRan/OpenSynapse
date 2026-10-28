package walkplay

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// fakePort answers reads with canned replies and records writes.
type fakePort struct {
	in     chan []byte
	sent   [][]byte
	answer func(req []byte) []byte
}

func (f *fakePort) send(r []byte) error {
	f.sent = append(f.sent, r)
	if a := f.answer(r); a != nil {
		f.in <- a
	}
	return nil
}

func (f *fakePort) replies() <-chan []byte { return f.in }

func hexBytes(t *testing.T, s string) []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return append(b, make([]byte, 64-len(b))...)
}

// A TP35 Pro reply for band 0 (31 Hz, Q 0.75, 0 dB, peak, preset 1).
const band0 = "4B 80 09 00 00 00 00 00 00 00 00 40 98 54 2C 80 A7 BC D3 3F 68 AB D3 7F 59 43 2C C0 1F 00 C0 00 00 00 02 00 01"

func TestDecodeBand(t *testing.T) {
	b, slot := decodeBand(hexBytes(t, band0))
	if b != (Band{Freq: 31, Q: 0.75, Gain: 0, Type: "peak"}) || slot != 1 {
		t.Fatalf("got %+v slot %d", b, slot)
	}
}

// The write of a band carries what a read returns, at the same offsets.
func TestEncodeBandRoundTrip(t *testing.T) {
	want := Band{Freq: 1000, Q: 1.41, Gain: -3.5, Type: "highShelf"}
	w := encodeBand(4, want, 1)
	reply := append([]byte{reportID}, w...) // a read reply has the write's layout
	got, slot := decodeBand(reply)
	if got != want || slot != 1 || w[4] != 4 {
		t.Fatalf("got %+v slot %d", got, slot)
	}
}

// A flat band's coefficients are what the dongle stores for it.
func TestBiquadMatchesDevice(t *testing.T) {
	got := biquad(Band{Freq: 31, Q: 0.75, Gain: 0, Type: "peak"})
	want := hexBytes(t, band0)[8:28]
	// The dongle's own rounding may differ in the last bit.
	for i := 0; i < 20; i += 4 {
		g := int32(uint32(got[i]) | uint32(got[i+1])<<8 | uint32(got[i+2])<<16 | uint32(got[i+3])<<24)
		w := int32(uint32(want[i]) | uint32(want[i+1])<<8 | uint32(want[i+2])<<16 | uint32(want[i+3])<<24)
		if d := g - w; d < -2 || d > 2 {
			t.Fatalf("coefficient %d: got %d, device %d", i/4, g, w)
		}
	}
}

func TestReadAndWriteEQ(t *testing.T) {
	f := &fakePort{in: make(chan []byte, 8)}
	f.answer = func(r []byte) []byte {
		if r[1] != opRead {
			return nil
		}
		switch r[2] {
		case cmdBand:
			a := hexBytes(t, band0)
			a[5] = r[5]
			return a
		case cmdPreamp:
			return hexBytes(t, "4B 80 03 02 00 FE")
		case cmdVersion:
			return hexBytes(t, "4B 80 0C 00 31 2E 37")
		}
		return nil
	}
	d := &Device{port: f}
	if v, err := d.Version(); err != nil || v != "1.7" {
		t.Fatalf("version %q %v", v, err)
	}
	e, err := d.EQ()
	if err != nil || len(e.Bands) != Bands || e.Preamp != -2 || d.slot != 1 {
		t.Fatalf("EQ %+v %v", e, err)
	}
	f.sent = nil
	e.Bands[3].Gain = 4
	if err := d.SetEQ(e); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != Bands+5 {
		t.Fatalf("%d reports sent", len(f.sent))
	}
	if b, _ := decodeBand(f.sent[3]); b.Gain != 4 || f.sent[3][36] != 1 {
		t.Fatalf("band 3 written as %+v, preset %d", b, f.sent[3][36])
	}
	if !bytes.HasPrefix(f.sent[Bands], []byte{reportID, opWrite, cmdPreamp, 0x02, 0x00, 0xFE}) {
		t.Fatalf("preamp write % X", f.sent[Bands][:6])
	}
	if !bytes.HasPrefix(f.sent[len(f.sent)-1], []byte{reportID, opWrite, cmdFlashEQ, 0x01}) {
		t.Fatal("EQ not saved last")
	}
	e.Bands[0].Q = 0
	if d.SetEQ(e) == nil {
		t.Fatal("Q 0 accepted")
	}
}

func TestCandidatePaths(t *testing.T) {
	for path, want := range map[string]bool{
		`\?\HID#VID_3302&PID_43E6&MI_00#b&34e80039&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`: true,
		`\?\hid#vid_0d8c&pid_43cc&mi_03#x`: true,  // another vendor, same family
		`\?\HID#VID_3302&PID_12E9&MI_00#x`: false, // another family
		`\?\HID#VID_1532&PID_43E6&MI_00#x`: false, // not a WalkPlay vendor
		`\?\HID#VID_33`:                    false,
	} {
		if got := candidate(path); got != want {
			t.Errorf("%s: %v", path, got)
		}
	}
}

package evision

import (
	"bytes"
	"testing"
)

// fakeV2 answers like OpenRGB expects V2 firmware to: requests echoed with
// the data read filled in, over a config space with profile 1 current.
type fakeV2 struct {
	t      *testing.T
	config []byte
	cmds   []byte
	in     chan []byte
}

func newFakeV2(t *testing.T) *fakeV2 {
	f := &fakeV2{t: t, config: make([]byte, 0xC1), in: make(chan []byte, 4)}
	f.config[0] = 1
	z := f.config[firstProfile+profileLen:]
	copy(z, []byte{0x05, 4, 3, 1, 0, 0x10, 0x20, 0x30, 9})
	z[parLEDMode] = 7
	return f
}

func (f *fakeV2) send(r []byte) error {
	if len(r) != reportLen || r[0] != reportID {
		f.t.Fatalf("report % x", r[:8])
	}
	var sum uint16
	for _, b := range r[3:] {
		sum += uint16(b)
	}
	if r[1] != byte(sum) || r[2] != byte(sum>>8) {
		f.t.Fatal("bad checksum")
	}
	f.cmds = append(f.cmds, r[3])
	a := append([]byte(nil), r...)
	off, size := int(r[5])|int(r[6])<<8, int(r[4])
	switch r[3] {
	case cmdCapabilites:
		copy(a[8:], []byte{0xAA, 0x55, 0, 0, 0, 126, 4})
	case cmdReadConfig:
		copy(a[8:8+size], f.config[off:])
	case cmdWriteConfig:
		copy(f.config[off:], r[8:8+size])
	}
	f.in <- a
	return nil
}

func (f *fakeV2) replies() <-chan []byte { return f.in }
func (f *fakeV2) close()                 {}

func TestV2(t *testing.T) {
	f := newFakeV2(t)
	d := &Device{port: f}
	if !d.identify() {
		t.Fatal("V2 not recognized")
	}
	l, err := d.Lighting()
	if err != nil || l != (Lighting{Effect: 5, Brightness: 4, Speed: 3, R: 0x10, G: 0x20, B: 0x30}) {
		t.Fatalf("read %+v %v", l, err)
	}
	if err := d.SetLighting(Lighting{Effect: 0x06, Brightness: 2, Speed: 0, Random: true, R: 0xff}); err != nil {
		t.Fatal(err)
	}
	z := f.config[firstProfile+profileLen:]
	if !bytes.Equal(z[:9], []byte{0x06, 2, 0, 1, 0xff, 0xff, 0, 0, 0}) || z[parLEDMode] != 7 {
		t.Fatalf("profile 1: % x", z[:zoneLen])
	}
	if !bytes.Contains(f.cmds, []byte{cmdReadConfig, cmdBegin, cmdWriteConfig, cmdEnd}) {
		t.Fatalf("commands % x", f.cmds)
	}
	if d.SetLighting(Lighting{Effect: 0x13}) == nil || d.SetLighting(Lighting{Effect: 6, Brightness: 5}) == nil {
		t.Fatal("accepted an unknown effect or brightness")
	}
}

// V1 is written its mode and settings at parameter 0 and reports them back from memory.
func TestV1(t *testing.T) {
	f := &v1Port{in: make(chan []byte, 2)}
	d := &Device{port: f}
	want := Lighting{Effect: 0x0A, Brightness: 3, Speed: 5, Random: true}
	if err := d.SetLighting(want); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 || f.sent[0][3] != cmdWriteConfig || f.sent[0][4] != 8 || !bytes.Equal(f.sent[0][8:16], []byte{0x0A, 3, 5, 0, 1, 0, 0, 0}) {
		t.Fatalf("sent % x", f.sent)
	}
	if l, _ := d.Lighting(); l != want {
		t.Fatalf("remembered %+v", l)
	}
}

type v1Port struct {
	sent [][]byte
	in   chan []byte
}

func (p *v1Port) send(r []byte) error {
	p.sent = append(p.sent, r)
	p.in <- make([]byte, reportLen)
	p.in <- append([]byte{reportID}, make([]byte, reportLen-1)...)
	return nil
}
func (p *v1Port) replies() <-chan []byte { return p.in }
func (p *v1Port) close()                 {}

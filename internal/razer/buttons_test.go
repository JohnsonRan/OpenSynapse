package razer

import (
	"bytes"
	"errors"
	"slices"
	"testing"
)

// Bytes as the DeathAdder V4 Pro (1532:00BF) sent and answered them. Set
// this way, button 5 sent F13.
func TestButtonCommands(t *testing.T) {
	var sent []Report
	reply := map[byte][]byte{
		0x84: {0x08, 0x01, 0x02, 0x03, 0x04, 0x05, 0x60, 0x09, 0x0A},
		0x8C: {0x00, 0x05, 0x01, 0x02, 0x02, 0x00, 0x68},
	}
	d := &Device{Model: &Model{tx: map[string]byte{}}, txOK: map[string]byte{}}
	d.sendFn = func(r Report) (Report, error) {
		sent = append(sent, r)
		r[offStatus] = statusOK
		if a, ok := reply[r[offCmd]]; ok {
			copy(r[offArgs:], a)
		}
		return r, nil
	}
	b, err := d.Buttons()
	if err != nil || !slices.Equal(b, []int{1, 2, 3, 4, 5, 0x60, 9, 10}) {
		t.Fatalf("buttons %v %v", b, err)
	}
	as, err := d.ButtonAssignment(5, LayerNormal)
	if err != nil || as.Fn != FnKey || !bytes.Equal(as.Data, []byte{0x00, 0x68}) {
		t.Fatalf("assignment %+v %v", as, err)
	}
	sent = nil
	if err := d.SetButtonAssignment(5, LayerNormal, Assignment{FnMouse, []byte{5}}); err != nil {
		t.Fatal(err)
	}
	want := NewReport(0x1F, 0x02, 0x0C, 80, 0, 5, 0, 1, 1, 5)
	if len(sent) != 1 || !bytes.Equal(sent[0][:], want[:]) {
		t.Errorf("sent %x", sent)
	}
}

// The clutch bytes follow Synapse's encoder; no device reply was recorded for
// them, so the write and the read-back below are synthesized.
func TestClutchAssignment(t *testing.T) {
	v4 := findModel(0x00BF)
	for _, c := range []struct {
		dpi  int
		data []byte
	}{
		{400, []byte{0x05, 0x01, 0x90, 0x01, 0x90}},
		{100, []byte{0x05, 0x00, 0x64, 0x00, 0x64}},
		{45000, []byte{0x05, 0xAF, 0xC8, 0xAF, 0xC8}},
	} {
		a, err := v4.ClutchAssignment(c.dpi)
		if err != nil || a.Fn != FnDPI || !bytes.Equal(a.Data, c.data) {
			t.Errorf("%d DPI: %+v %v, want function 6 with data % x", c.dpi, a, err, c.data)
		}
		if dpi, ok := v4.ClutchDPI(a); !ok || dpi != c.dpi {
			t.Errorf("%d DPI read back as %d %v", c.dpi, dpi, ok)
		}
	}
	for _, dpi := range []int{0, 99, 45001, 65536 + 400} {
		if a, err := v4.ClutchAssignment(dpi); err == nil {
			t.Errorf("%d DPI accepted as % x", dpi, a.Data)
		}
	}

	// The whole report of the write, and the mouse's answer to the read.
	var sent []Report
	d := &Device{Model: v4, txOK: map[string]byte{}}
	d.sendFn = func(r Report) (Report, error) {
		sent = append(sent, r)
		r[offStatus] = statusOK
		if r[offCmd] == 0x8C {
			copy(r[offArgs:], []byte{0x00, 0x04, 0x01, 0x06, 0x05, 0x05, 0x01, 0x90, 0x01, 0x90})
		}
		return r, nil
	}
	a, _ := v4.ClutchAssignment(400)
	if err := d.SetButtonAssignment(4, LayerNormal, a); err != nil {
		t.Fatal(err)
	}
	// profile 0, button 4, layer 0, function 6, 5 data bytes: clutch, 400, 400
	want := NewReport(0x1F, 0x02, 0x0C, 80, 0x00, 0x04, 0x00, 0x06, 0x05, 0x05, 0x01, 0x90, 0x01, 0x90)
	if len(sent) != 1 || sent[0] != want {
		t.Errorf("sent % x", sent[0][:20])
	}
	back, err := d.ButtonAssignment(4, LayerNormal)
	if dpi, ok := v4.ClutchDPI(back); err != nil || !ok || dpi != 400 {
		t.Errorf("read back %+v %v: %d %v, want a clutch at 400", back, err, dpi, ok)
	}

	// Not a clutch the app can write again: another model, X and Y apart, a
	// DPI out of range, other lengths, the plain DPI actions.
	clutch := func(data ...byte) Assignment { return Assignment{Fn: FnDPI, Data: data} }
	other := &Model{Name: "Razer Viper V3 Pro (Wired)", Category: "mouse", MaxDPI: 35000}
	if _, ok := other.ClutchDPI(a); ok {
		t.Error("a clutch on a model that is not offered it")
	}
	if _, err := other.ClutchAssignment(400); !errors.Is(err, ErrUnsupported) {
		t.Errorf("a clutch for a model that is not offered it: %v", err)
	}
	if !v4.Features().Clutch || !findModel(0x00BE).Features().Clutch || other.Features().Clutch {
		t.Error("the clutch is offered on the DeathAdder V4 Pro alone")
	}
	for name, as := range map[string]Assignment{
		"X and Y apart":  clutch(5, 0x01, 0x90, 0x03, 0x20),
		"below 100":      clutch(5, 0x00, 0x32, 0x00, 0x32),
		"above the max":  clutch(5, 0xAF, 0xC9, 0xAF, 0xC9),
		"short":          clutch(5, 0x01, 0x90),
		"long":           clutch(5, 0x01, 0x90, 0x01, 0x90, 0x00),
		"cycle up":       clutch(6),
		"other action":   clutch(4, 0x01, 0x90, 0x01, 0x90),
		"other function": {Fn: FnKey, Data: []byte{5, 0x01, 0x90, 0x01, 0x90}},
	} {
		if dpi, ok := v4.ClutchDPI(as); ok {
			t.Errorf("%s: read as a clutch at %d", name, dpi)
		}
	}
}

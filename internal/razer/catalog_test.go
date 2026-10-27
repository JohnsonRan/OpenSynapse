package razer

import (
	"bytes"
	"slices"
	"testing"
)

func model(t *testing.T, pid uint16) *Model {
	m := findModel(pid)
	if m == nil {
		t.Fatalf("%04X missing from the table", pid)
	}
	return m
}

func TestCatalogCoverage(t *testing.T) {
	if len(Models) < 200 {
		t.Fatalf("only %d models loaded", len(Models))
	}
	for _, m := range Models {
		if m.Category == "laptop" {
			t.Fatalf("%04X: laptops are excluded", m.PID)
		}
	}
	f := model(t, 0x00BF).Features()
	if !f.DPIStages || !f.PollRate || !f.Battery || !f.ReceiverLEDs || f.Lighting != nil {
		t.Fatalf("DeathAdder V4 Pro features: %+v", f)
	}
}

func TestLightingZones(t *testing.T) {
	cases := []struct {
		pid    uint16
		family string
		zones  []byte
	}{
		{0x024E, famExtended, []byte{0x05}},       // BlackWidow V3: backlight
		{0x0099, famExtended, []byte{0x00}},       // Basilisk V3: whole device
		{0x0084, famExtended, []byte{0x01, 0x04}}, // DeathAdder V2: scroll + logo
		{0x0015, famState, []byte{0x01, 0x04, 0x05}},
	}
	for _, c := range cases {
		m := model(t, c.pid)
		if f := m.lightingFamily(); f != c.family || !slices.Equal(m.lightingZones(f), c.zones) {
			t.Errorf("%04X: family %q zones %x", c.pid, f, m.lightingZones(f))
		}
	}
	if !slices.Contains(model(t, 0x024E).lightingCaps().Effects, EffectStatic) {
		t.Error("static must be offered on matrix keyboards")
	}
}

func TestSetLightingPacket(t *testing.T) {
	m := model(t, 0x024E)
	var sent []Report
	d := &Device{Model: m, txOK: map[string]byte{}}
	d.sendFn = func(r Report) (Report, error) { sent = append(sent, r); r[offStatus] = statusOK; return r, nil }
	if err := d.SetLighting(EffectStatic, RGB{0x44, 0xD6, 0x2C}); err != nil {
		t.Fatal(err)
	}
	// extended_matrix_effect_static(VARSTORE, BACKLIGHT_LED, rgb), per openrazer.
	want := NewReport(m.tx["extended_matrix_effect_none"], 0x0F, 0x02, 0x09, 0x01, 0x05, 0x01, 0, 0, 0x01, 0x44, 0xD6, 0x2C)
	if len(sent) != 1 || !bytes.Equal(sent[0][:], want[:]) {
		t.Fatalf("sent %x\nwant %x", sent, want)
	}
}

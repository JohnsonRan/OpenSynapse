package razer

import (
	"bytes"
	"testing"
)

func TestTrackingSymmetric(t *testing.T) {
	sensorID := byte(0x01)
	reply := map[byte][]byte{
		0x80: {0x01, sensorID},                    // sensorID query: 1 sensor, id 1
		0x83: {sensorClass, sensorID, 0x00},       // state: symmetric (0)
		0x8B: {sensorClass, sensorID, 0x01, 0x01}, // lift: cutOff=1, distance-1=1 (medium = 2)
	}
	var sent []Report
	d := &Device{Model: &Model{tx: map[string]byte{}}, txOK: map[string]byte{}}
	d.sendFn = func(r Report) (Report, error) {
		sent = append(sent, r)
		r[offStatus] = statusOK
		if a, ok := reply[r[offCmd]]; ok {
			copy(r[offArgs:], a)
		}
		return r, nil
	}

	tr, err := d.Tracking()
	if err != nil {
		t.Fatalf("Tracking: %v", err)
	}
	if tr.Asymmetric || tr.Distance != 2 {
		t.Fatalf("got %+v, want symmetric distance 2", tr)
	}

	sent = nil
	if err := d.SetTracking(Tracking{Distance: 3}); err != nil {
		t.Fatalf("SetTracking: %v", err)
	}
	// Expected: set 0x03 (disable asym), set 0x0B (distance-1 = 2)
	if len(sent) != 3 { // sensorID query + 2 set commands
		t.Fatalf("sent %d reports, want 3", len(sent))
	}
	wantCmd03 := NewReport(0x1F, 0x0B, 0x03, 3, sensorClass, sensorID, 0)
	wantCmd0B := NewReport(0x1F, 0x0B, 0x0B, 4, sensorClass, sensorID, liftCutOff, 2)
	if !bytes.Equal(sent[1][:], wantCmd03[:]) {
		t.Errorf("sent[1] = %x, want %x", sent[1], wantCmd03)
	}
	if !bytes.Equal(sent[2][:], wantCmd0B[:]) {
		t.Errorf("sent[2] = %x, want %x", sent[2], wantCmd0B)
	}
}

func TestTrackingAsymmetric(t *testing.T) {
	sensorID := byte(0x01)
	reply := map[byte][]byte{
		0x80: {0x01, sensorID},
		0x83: {sensorClass, sensorID, 0x01},                 // asymmetric (1)
		0x8B: {sensorClass, sensorID, 0x01, 0x00},           // preset (ignored for asym)
		0x85: {sensorClass, sensorID, 0x00, 13 - 1, 12 - 1}, // cfg: extra byte, liftOff-1, landing-1
	}
	var sent []Report
	d := &Device{Model: &Model{tx: map[string]byte{}}, txOK: map[string]byte{}}
	d.sendFn = func(r Report) (Report, error) {
		sent = append(sent, r)
		r[offStatus] = statusOK
		if a, ok := reply[r[offCmd]]; ok {
			copy(r[offArgs:], a)
		}
		return r, nil
	}

	tr, err := d.Tracking()
	if err != nil {
		t.Fatalf("Tracking: %v", err)
	}
	if !tr.Asymmetric || tr.LiftOff != 13 || tr.Landing != 12 {
		t.Fatalf("got %+v, want asymmetric liftOff 13 landing 12", tr)
	}

	sent = nil
	if err := d.SetTracking(Tracking{Asymmetric: true, LiftOff: 20, Landing: 10}); err != nil {
		t.Fatalf("SetTracking: %v", err)
	}
	// Expected: sensor query + set 0x03 (enable asym) + set 0x0B (hysteresis 0) + set 0x05 (19, 9)
	if len(sent) != 4 {
		t.Fatalf("sent %d reports, want 4", len(sent))
	}
	wantCmd03 := NewReport(0x1F, 0x0B, 0x03, 3, sensorClass, sensorID, 1)
	wantCmd0B := NewReport(0x1F, 0x0B, 0x0B, 4, sensorClass, sensorID, liftPresetHyster, 0)
	wantCmd05 := NewReport(0x1F, 0x0B, 0x05, 10, sensorClass, sensorID, 19, 9)
	if !bytes.Equal(sent[1][:], wantCmd03[:]) {
		t.Errorf("sent[1] = %x, want %x", sent[1], wantCmd03)
	}
	if !bytes.Equal(sent[2][:], wantCmd0B[:]) {
		t.Errorf("sent[2] = %x, want %x", sent[2], wantCmd0B)
	}
	if !bytes.Equal(sent[3][:], wantCmd05[:]) {
		t.Errorf("sent[3] = %x, want %x", sent[3], wantCmd05)
	}
}

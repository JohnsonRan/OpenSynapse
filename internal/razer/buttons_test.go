package razer

import (
	"bytes"
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

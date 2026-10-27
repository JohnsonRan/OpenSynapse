package razer

import "testing"

func TestNewReportLayoutAndCRC(t *testing.T) {
	// set_polling_rate2(1000 Hz, receiver) as openrazer documents it.
	r := NewReport(0x1F, 0x00, 0x40, 0x02, 0x01, 0x08)
	want := map[int]byte{0: 0, 2: 0x1F, 6: 0x02, 7: 0x00, 8: 0x40, 9: 0x01, 10: 0x08, 89: 0x02 ^ 0x40 ^ 0x01 ^ 0x08}
	for i, v := range want {
		if r[i] != v {
			t.Errorf("byte %d = %#x, want %#x", i, r[i], v)
		}
	}
	resp := r
	resp[offStatus] = statusOK
	if err := resp.check(&r); err != nil {
		t.Fatal(err)
	}
	resp[offCRC] ^= 1
	if resp.check(&r) == nil {
		t.Fatal("bad CRC accepted")
	}
}

func TestStagesRoundTrip(t *testing.T) {
	in := []DPIStage{{400, 400}, {1600, 1600}, {45000, 30000}}
	args := encodeStages(in, 2)
	// A device answers in the same layout, with 1-based stage numbers.
	var resp [80]byte
	copy(resp[:], args)
	out, active := decodeStages(resp[:], 0x26)
	if active != 2 || len(out) != 3 || out[2] != in[2] || out[0] != in[0] {
		t.Fatalf("got %v active %d", out, active)
	}
}

func TestPollCodes(t *testing.T) {
	for hz, c := range pollCodes {
		if pollFromCode(c) != hz {
			t.Errorf("%d", hz)
		}
	}
}

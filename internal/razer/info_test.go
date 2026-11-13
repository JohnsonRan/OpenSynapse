package razer

import (
	"bytes"
	"errors"
	"runtime"
	"sync"
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

// What the owner's DeathAdder V4 Pro (receiver 1532:00BF, firmware 1.21,
// 2026-11-14) answered to the rotation get, from the status byte on: status
// OK, transaction 1F, size 3, class 0B, command 94, profile 1, off, angle 0.
var recordedRotationReply = []byte{0x02, 0x1F, 0x00, 0x00, 0x00, 0x03, 0x0B, 0x94, 0x01, 0x00, 0x00}

// rotationMouse stands in for a DeathAdder V4 Pro. Its answer to the get is
// recordedRotationReply with the state and angle it holds. Everything else is
// synthesized, not recorded: the sensor id answer, the accepted set, and the
// values other than "off, 0".
type rotationMouse struct {
	mu           sync.Mutex
	sent         []Report
	state, angle byte
	onSet        func(m *rotationMouse, req Report)   // nil: keep what was written
	tweak        func(req Report, resp *Report) error // changes a reply or fails the request
	sets         int                                  // sets seen
	pending      bool                                 // a set has not been read back yet
	overlaps     int                                  // sets that arrived while one was pending
}

func (m *rotationMouse) handle(req Report) (Report, error) {
	runtime.Gosched() // lets concurrent writers interleave
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, req)
	resp := req
	resp[offStatus] = statusOK
	switch req[offCmd] {
	case 0x80:
		copy(resp[offArgs:], []byte{0x01, 0x04})
	case 0x94:
		copy(resp[offStatus:], recordedRotationReply)
		resp[offArgs+1], resp[offArgs+2] = m.state, m.angle
		m.pending = false
	case 0x14:
		if m.pending {
			m.overlaps++
		}
		m.sets++
		m.pending = true
		if m.onSet != nil {
			m.onSet(m, req)
		} else {
			m.state, m.angle = req[offArgs+1], req[offArgs+2]
		}
	}
	if m.tweak != nil {
		if err := m.tweak(req, &resp); err != nil {
			return Report{}, err
		}
	}
	return resp, nil
}

func (m *rotationMouse) device(model *Model) *Device {
	return &Device{Model: model, txOK: map[string]byte{}, sendFn: m.handle}
}

// cmds lists the command ids sent, in order.
func (m *rotationMouse) cmds() []byte {
	out := make([]byte, len(m.sent))
	for i, r := range m.sent {
		out[i] = r[offCmd]
	}
	return out
}

func TestRotationRead(t *testing.T) {
	for _, pid := range []uint16{0x00BE, 0x00BF} {
		m := &rotationMouse{}
		d := m.device(findModel(pid))
		r, err := d.Rotation()
		if err != nil || r != (Rotation{}) {
			t.Fatalf("%04X: %+v %v, want off at 0 as recorded", pid, r, err)
		}
		if !bytes.Equal(m.cmds(), []byte{0x80, 0x94}) {
			t.Fatalf("%04X: sent commands %x, want the sensor query and the get", pid, m.cmds())
		}
		get := m.sent[1]
		// transaction, three reserved bytes, size, class, command, profile
		if !bytes.Equal(get[offTxID:offArgs+2], []byte{0x1F, 0, 0, 0, 0x03, 0x0B, 0x94, 0x01, 0x00}) || get[offCRC] != 0x03^0x0B^0x94^0x01 {
			t.Errorf("%04X: get request % x", pid, get[:12])
		}
	}
	for _, c := range []struct {
		state, angle byte
		want         Rotation
	}{
		{1, 0x14, Rotation{true, 20}},
		{1, 0xEC, Rotation{true, -20}},
		{1, 0x2C, Rotation{true, 44}},
		{1, 0xD4, Rotation{true, -44}},
		{0, 0x14, Rotation{false, 20}},
	} {
		m := &rotationMouse{state: c.state, angle: c.angle}
		if r, err := m.device(findModel(0x00BF)).Rotation(); err != nil || r != c.want {
			t.Errorf("state %d angle %#x: %+v %v, want %+v", c.state, c.angle, r, err, c.want)
		}
	}
}

// A reply that is not a complete rotation answer means "unsupported": the
// read fails and no write follows it. All of these replies are synthesized.
func TestRotationStrictReply(t *testing.T) {
	get := func(change func(resp *Report)) func(Report, *Report) error {
		return func(req Report, resp *Report) error {
			if req[offCmd] == 0x94 {
				change(resp)
			}
			return nil
		}
	}
	cases := map[string]func(Report, *Report) error{
		"busy":      get(func(r *Report) { r[offStatus] = statusBusy }),
		"all zero":  get(func(r *Report) { *r = Report{} }),
		"size 2":    get(func(r *Report) { r[offSize] = 2 }),
		"profile 0": get(func(r *Report) { r[offArgs] = 0 }),
		"profile 2": get(func(r *Report) { r[offArgs] = 2 }),
		"state 2":   get(func(r *Report) { r[offArgs+1] = 2 }),
		"angle 45":  get(func(r *Report) { r[offArgs+2] = 45 }),
		"angle -45": get(func(r *Report) { r[offArgs+2] = 0xD3 }),
		"no sensor id": func(req Report, resp *Report) error {
			if req[offCmd] == 0x80 {
				resp[offArgs], resp[offArgs+1] = 0, 0
			}
			return nil
		},
	}
	for name, tweak := range cases {
		m := &rotationMouse{tweak: tweak}
		d := m.device(findModel(0x00BF))
		if _, err := d.Rotation(); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: read %v, want ErrUnsupported", name, err)
		}
		if err := d.SetRotation(Rotation{Enabled: true, Angle: 20}); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: write %v, want ErrUnsupported", name, err)
		}
		if m.sets != 0 {
			t.Errorf("%s: %d writes reached a device that does not answer the read", name, m.sets)
		}
		if name == "no sensor id" && bytes.Contains(m.cmds(), []byte{0x94}) {
			t.Errorf("%s: the get was sent without a sensor", name)
		}
	}

	// A sleeping mouse is not an unsupported one.
	m := &rotationMouse{tweak: func(req Report, _ *Report) error {
		if req[offCmd] == 0x94 {
			return ErrNoAnswer
		}
		return nil
	}}
	d := m.device(findModel(0x00BF))
	if _, err := d.Rotation(); !errors.Is(err, ErrNoAnswer) || errors.Is(err, ErrUnsupported) {
		t.Errorf("asleep: read %v, want ErrNoAnswer", err)
	}
	if err := d.SetRotation(Rotation{Enabled: true, Angle: 20}); !errors.Is(err, ErrNoAnswer) || m.sets != 0 {
		t.Errorf("asleep: write %v with %d sets, want ErrNoAnswer and none", err, m.sets)
	}
}

// Only the DeathAdder V4 Pro is sent the rotation commands.
func TestRotationModelGate(t *testing.T) {
	for _, model := range []*Model{
		{Name: "Razer DeathAdder V3 Pro (Wired)", Category: "mouse"},
		{Name: "Razer Viper V3 Pro (Wireless)", Category: "mouse"},
		{Name: "Razer DeathAdder V4 Pro", Category: "keyboard"},
	} {
		m := &rotationMouse{}
		d := m.device(model)
		if _, err := d.Rotation(); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s (%s): read %v, want ErrUnsupported", model.Name, model.Category, err)
		}
		if err := d.SetRotation(Rotation{Enabled: true, Angle: 20}); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s (%s): write %v, want ErrUnsupported", model.Name, model.Category, err)
		}
		if len(m.sent) != 0 {
			t.Errorf("%s (%s): sent %x", model.Name, model.Category, m.cmds())
		}
	}
}

func TestSetRotationBytes(t *testing.T) {
	for _, c := range []struct {
		r    Rotation
		args []byte // profile, state, angle
	}{
		{Rotation{true, -10}, []byte{0x01, 0x01, 0xF6}},
		{Rotation{true, 20}, []byte{0x01, 0x01, 0x14}},
		{Rotation{true, -20}, []byte{0x01, 0x01, 0xEC}},
		{Rotation{true, 44}, []byte{0x01, 0x01, 0x2C}},
		{Rotation{true, -44}, []byte{0x01, 0x01, 0xD4}},
		{Rotation{false, 20}, []byte{0x01, 0x00, 0x14}},
		{Rotation{false, 0}, []byte{0x01, 0x00, 0x00}}, // Synapse's default, the way back
	} {
		m := &rotationMouse{state: 1, angle: 7}
		if err := m.device(findModel(0x00BF)).SetRotation(c.r); err != nil {
			t.Errorf("%+v: %v", c.r, err)
			continue
		}
		// sensor query and get (the value replaced), set, read-back
		if !bytes.Equal(m.cmds(), []byte{0x80, 0x94, 0x14, 0x94}) {
			t.Errorf("%+v: sent commands %x", c.r, m.cmds())
			continue
		}
		set := m.sent[2]
		want := NewReport(0x1F, 0x0B, 0x14, 3, c.args...)
		if set != want || !bytes.Equal(set[offSize:offArgs+3], append([]byte{0x03, 0x0B, 0x14}, c.args...)) {
			t.Errorf("%+v: set request % x, want % x", c.r, set[:13], want[:13])
		}
	}
	// 212 would wrap to -44 as an int8.
	for _, angle := range []int{45, -45, 212, -212} {
		m := &rotationMouse{}
		if err := m.device(findModel(0x00BF)).SetRotation(Rotation{Enabled: true, Angle: angle}); err == nil || len(m.sent) != 0 {
			t.Errorf("angle %d: %v after %d requests, want a refusal before any", angle, err, len(m.sent))
		}
	}
}

// The write is read back; a value the device did not keep is replaced by the
// old one, once.
func TestSetRotationRollback(t *testing.T) {
	old := NewReport(0x1F, 0x0B, 0x14, 3, 0x01, 0x01, 0x05)
	for name, onSet := range map[string]func(*rotationMouse, Report){
		"write ignored": func(*rotationMouse, Report) {},
		"other angle":   func(m *rotationMouse, req Report) { m.state, m.angle = req[offArgs+1], req[offArgs+2]+1 },
		"other state":   func(m *rotationMouse, req Report) { m.state, m.angle = 0, req[offArgs+2] },
	} {
		m := &rotationMouse{state: 1, angle: 5, onSet: onSet}
		err := m.device(findModel(0x00BF)).SetRotation(Rotation{Enabled: true, Angle: 20})
		if !errors.Is(err, ErrNotKept) {
			t.Errorf("%s: %v, want ErrNotKept", name, err)
		}
		if !bytes.Equal(m.cmds(), []byte{0x80, 0x94, 0x14, 0x94, 0x14}) || m.sent[4] != old {
			t.Errorf("%s: sent commands %x, last % x; want one write of the old value", name, m.cmds(), m.sent[len(m.sent)-1][:13])
		}
	}

	// Turned off, the firmware may report another angle than the one written.
	m := &rotationMouse{state: 1, angle: 5, onSet: func(m *rotationMouse, req Report) { m.state, m.angle = req[offArgs+1], 0 }}
	if err := m.device(findModel(0x00BF)).SetRotation(Rotation{Enabled: false, Angle: 20}); err != nil || m.sets != 1 {
		t.Errorf("off at 20 read back as off at 0: %v with %d writes, want it accepted", err, m.sets)
	}

	// No answer to the read-back or to the write: the error is reported and
	// nothing is written again.
	for name, fail := range map[string]func(m *rotationMouse, req Report) bool{
		"read-back": func(m *rotationMouse, req Report) bool { return req[offCmd] == 0x94 && m.sets > 0 },
		"write":     func(m *rotationMouse, req Report) bool { return req[offCmd] == 0x14 },
	} {
		m := &rotationMouse{}
		m.tweak = func(req Report, _ *Report) error {
			if fail(m, req) {
				return ErrNoAnswer
			}
			return nil
		}
		if err := m.device(findModel(0x00BF)).SetRotation(Rotation{Enabled: true, Angle: 20}); !errors.Is(err, ErrNoAnswer) {
			t.Errorf("no answer to the %s: %v, want ErrNoAnswer", name, err)
		}
		if writes := bytes.Count(distinctTx(m.sent), []byte{0x14}); writes != 1 {
			t.Errorf("no answer to the %s: the value was written %d times, want once", name, writes)
		}
	}
}

// distinctTx lists the command ids sent, counting a request repeated under
// the next transaction id (run does that after no answer) once.
func distinctTx(sent []Report) []byte {
	var out []byte
	for i, r := range sent {
		if i > 0 && r[offCmd] == sent[i-1][offCmd] && r[offTxID] != sent[i-1][offTxID] {
			continue
		}
		out = append(out, r[offCmd])
	}
	return out
}

// Two writes at once would read back each other's value and undo it.
func TestSetRotationSerialized(t *testing.T) {
	m := &rotationMouse{}
	d := m.device(findModel(0x00BF))
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 100 {
				if err := d.SetRotation(Rotation{Enabled: true, Angle: (g*100+i)%89 - 44}); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	if m.overlaps != 0 {
		t.Errorf("%d writes arrived before the one before them was read back", m.overlaps)
	}
}

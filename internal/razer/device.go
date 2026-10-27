package razer

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Model describes one supported product. Add a row to support another device
// that uses the same command set.
type Model struct {
	PID       uint16
	Name      string
	TxID      byte
	MaxDPI    int
	PollRates []int
	// Wireless: the HyperPolling receiver keeps its own poll-rate copy (target 0x01).
	Wireless bool
}

// Models is the device table. Values from openrazer's DeathAdder V4 Pro entries.
var Models = []Model{
	{PID: 0x00BE, Name: "Razer DeathAdder V4 Pro (Wired)", TxID: 0x1F, MaxDPI: 45000, PollRates: []int{125, 500, 1000, 2000, 4000, 8000}},
	{PID: 0x00BF, Name: "Razer DeathAdder V4 Pro", TxID: 0x1F, MaxDPI: 45000, PollRates: []int{125, 500, 1000, 2000, 4000, 8000}, Wireless: true},
}

func findModel(pid uint16) *Model {
	for i := range Models {
		if Models[i].PID == pid {
			return &Models[i]
		}
	}
	return nil
}

// Device is a connected supported device.
type Device struct {
	Model
	ID         string
	candidates []*conn // feature collections; the first that answers wins
	mu         sync.Mutex
	active     *conn
}

func (d *Device) query(class, cmd, size byte, args ...byte) (Report, error) {
	req := NewReport(d.TxID, class, cmd, size, args...)
	d.mu.Lock()
	c := d.active
	d.mu.Unlock()
	if c != nil {
		return c.transact(req)
	}
	var err error
	for _, c := range d.candidates {
		var r Report
		if r, err = c.transact(req); err == nil || errors.Is(err, ErrUnsupported) {
			d.mu.Lock()
			d.active = c
			d.mu.Unlock()
			return r, err
		}
	}
	if err == nil {
		err = errors.New("no usable HID collection")
	}
	return Report{}, err
}

// Candidates reports how many feature collections matched (diagnostics).
func Candidates(d *Device) int { return len(d.candidates) }

func (d *Device) Close() {
	for _, c := range d.candidates {
		c.close()
	}
}

// DPI returns the current X/Y sensitivity.
func (d *Device) DPI() (DPIStage, error) {
	r, err := d.query(0x04, 0x85, 0x07, noStore)
	if err != nil {
		return DPIStage{}, err
	}
	a := r.Args()
	return DPIStage{X: be16(a[1:]), Y: be16(a[3:])}, nil
}

func (d *Device) SetDPI(x, y int) error {
	if err := d.checkDPI(x, y); err != nil {
		return err
	}
	args := append([]byte{varStore}, put16(x)...)
	args = append(args, put16(y)...)
	_, err := d.query(0x04, 0x05, 0x07, append(args, 0, 0)...)
	return err
}

func (d *Device) checkDPI(x, y int) error {
	if x < 100 || y < 100 || x > d.MaxDPI || y > d.MaxDPI {
		return fmt.Errorf("DPI must be 100–%d", d.MaxDPI)
	}
	return nil
}

// DPIStages returns stages and the 1-based active stage.
func (d *Device) DPIStages() ([]DPIStage, int, error) {
	r, err := d.query(0x04, 0x86, 0x26, varStore)
	if err != nil {
		return nil, 0, err
	}
	s, active := decodeStages(r.Args(), int(r[offSize]))
	return s, active, nil
}

// SetDPIStages writes 1–5 stages; active is 1-based.
func (d *Device) SetDPIStages(stages []DPIStage, active int) error {
	if len(stages) < 1 || len(stages) > 5 || active < 1 || active > len(stages) {
		return errors.New("need 1–5 stages and a valid active stage")
	}
	for _, s := range stages {
		if err := d.checkDPI(s.X, s.Y); err != nil {
			return err
		}
	}
	_, err := d.query(0x04, 0x06, 0x26, encodeStages(stages, active)...)
	return err
}

func (d *Device) PollRate() (int, error) {
	r, err := d.query(0x00, 0xC0, 0x01)
	if err != nil {
		return 0, err
	}
	return pollFromCode(r.Args()[1]), nil
}

func (d *Device) SetPollRate(hz int) error {
	code, ok := pollCodes[hz]
	if !ok || !contains(d.PollRates, hz) {
		return fmt.Errorf("unsupported polling rate %d", hz)
	}
	// Target 0x00 is the mouse, 0x01 the receiver; Synapse writes both.
	if _, err := d.query(0x00, 0x40, 0x02, 0x00, code); err != nil {
		return err
	}
	if d.Wireless {
		_, err := d.query(0x00, 0x40, 0x02, 0x01, code)
		return err
	}
	return nil
}

// Battery returns 0–100.
func (d *Device) Battery() (int, error) {
	r, err := d.query(0x07, 0x80, 0x02)
	if err != nil {
		return 0, err
	}
	return int(r.Args()[1]) * 100 / 255, nil
}

func (d *Device) Charging() (bool, error) {
	r, err := d.query(0x07, 0x84, 0x02)
	return err == nil && r.Args()[1] == 1, err
}

// IdleTime is the seconds before the mouse sleeps (60–900).
func (d *Device) IdleTime() (int, error) {
	r, err := d.query(0x07, 0x83, 0x02)
	if err != nil {
		return 0, err
	}
	return be16(r.Args()), nil
}

func (d *Device) SetIdleTime(sec int) error {
	if sec < 60 || sec > 900 {
		return errors.New("idle time must be 60–900 s")
	}
	_, err := d.query(0x07, 0x03, 0x02, put16(sec)...)
	return err
}

// LowBattery returns the low-battery warning threshold in percent.
func (d *Device) LowBattery() (int, error) {
	r, err := d.query(0x07, 0x81, 0x01)
	if err != nil {
		return 0, err
	}
	return (int(r.Args()[0])*100 + 127) / 255, nil
}

// SetLowBattery accepts 5–30 percent. openrazer clamps to 25%, but the
// DeathAdder V4 Pro shipped reporting 30%, so the firmware takes it.
func (d *Device) SetLowBattery(pct int) error {
	if pct < 5 || pct > 30 {
		return errors.New("threshold must be 5–30%")
	}
	_, err := d.query(0x07, 0x01, 0x01, byte((pct*255+50)/100))
	return err
}

func contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Scan finds connected supported devices.
func Scan() ([]*Device, error) {
	paths, err := hidPaths()
	if err != nil {
		return nil, err
	}
	byPID := map[uint16]*Device{}
	keys := make([]string, 0, len(paths))
	for p := range paths {
		keys = append(keys, p)
	}
	sort.Strings(keys) // stable order: MI_00 collections first
	for _, p := range keys {
		m := findModel(paths[p])
		if m == nil {
			continue
		}
		d := byPID[m.PID]
		if d == nil {
			d = &Device{Model: *m, ID: fmt.Sprintf("1532:%04X", m.PID)}
			byPID[m.PID] = d
		}
		d.candidates = append(d.candidates, &conn{path: p})
	}
	out := make([]*Device, 0, len(byPID))
	for _, d := range byPID {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

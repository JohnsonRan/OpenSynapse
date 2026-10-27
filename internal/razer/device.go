package razer

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Device is a connected supported device.
type Device struct {
	*Model
	ID         string
	candidates []*conn // feature collections; the first that answers wins
	mu         sync.Mutex
	active     *conn
	txOK       map[string]byte              // transaction id that worked, per command
	sendFn     func(Report) (Report, error) // tests replace the HID transport
	// Answered: the device replied when scanned. A wireless receiver stays
	// attached while its mouse is off or on a cable, but cannot reply for it.
	Answered bool
}

// probe asks something only the device itself can answer (a receiver may
// answer firmware queries on its own), which also pins the HID collection.
func (d *Device) probe() error {
	if d.has("misc_get_dpi_xy") {
		_, err := d.DPI()
		return err
	}
	_, err := d.query("standard_get_firmware_version", 0x00, 0x81, 0x02)
	return err
}

// Family names the product without its connection, so the wired and wireless
// IDs of one mouse can be paired.
func (m *Model) Family() string {
	n := m.Name
	for _, suffix := range []string{" (Wired)", " (Wireless)", " (Receiver)", " (Bluetooth)"} {
		n = strings.TrimSuffix(n, suffix)
	}
	return n
}

// Wired reports whether this ID is the cable connection of a wireless product.
func (m *Model) Wired() bool { return strings.HasSuffix(m.Name, " (Wired)") }

// txCandidates: the driver's id first, then the ids devices commonly answer,
// in case the driver's default branch hid the real one: 0x1F (current
// devices), 0xFF (older ones), 0x3F and 0x9F (some keyboards and receivers).
func (d *Device) txCandidates(name string) []byte {
	if t, ok := d.txOK[name]; ok {
		return []byte{t}
	}
	out := []byte{d.tx[name]}
	for _, t := range []byte{0x1F, 0xFF, 0x3F, 0x9F} {
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// query runs the named openrazer command. Commands the device does not declare
// are never sent.
func (d *Device) query(name string, class, cmd, size byte, args ...byte) (Report, error) {
	if !d.has(name) {
		return Report{}, ErrUnsupported
	}
	var err error
	for _, tx := range d.txCandidates(name) {
		var r Report
		if r, err = d.send(NewReport(tx, class, cmd, size, args...)); err == nil {
			d.mu.Lock()
			d.txOK[name] = tx
			d.mu.Unlock()
			return r, nil
		}
		if !errors.Is(err, ErrNoAnswer) {
			return r, err
		}
	}
	return Report{}, err
}

func (d *Device) send(req Report) (Report, error) {
	if d.sendFn != nil {
		return d.sendFn(req)
	}
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

// Features lists the controls the device declares.
type Features struct {
	DPI          bool          `json:"dpi"`
	DPIStages    bool          `json:"dpiStages"`
	PollRate     bool          `json:"pollRate"`
	Battery      bool          `json:"battery"`
	Charging     bool          `json:"charging"`
	IdleTime     bool          `json:"idleTime"`
	LowBattery   bool          `json:"lowBattery"`
	ReceiverLEDs bool          `json:"receiverLeds"`
	Lighting     *LightingCaps `json:"lighting"`
}

func (m *Model) Features() Features {
	return Features{
		DPI:          m.has("misc_get_dpi_xy") && m.has("misc_set_dpi_xy"),
		DPIStages:    m.has("misc_get_dpi_stages") && m.has("misc_set_dpi_stages"),
		PollRate:     m.pollV2() || m.has("misc_set_polling_rate"),
		Battery:      m.has("misc_get_battery_level"),
		Charging:     m.has("misc_get_charging_status"),
		IdleTime:     m.has("misc_get_idle_time") && m.has("misc_set_idle_time"),
		LowBattery:   m.has("misc_get_low_battery_threshold") && m.has("misc_set_low_battery_threshold"),
		ReceiverLEDs: m.receivers,
		Lighting:     m.lightingCaps(),
	}
}

// SupportedPollRates is what the UI may offer.
func (m *Model) SupportedPollRates() []int {
	switch {
	case len(m.PollRates) > 0:
		return m.PollRates
	case m.pollV2():
		return []int{125, 500, 1000, 2000, 4000, 8000}
	case m.has("misc_set_polling_rate"):
		return []int{125, 500, 1000}
	}
	return nil
}

func (m *Model) pollV2() bool { return m.has("misc_set_polling_rate2") }

// DPI returns the current X/Y sensitivity.
func (d *Device) DPI() (DPIStage, error) {
	r, err := d.query("misc_get_dpi_xy", 0x04, 0x85, 0x07, noStore)
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
	_, err := d.query("misc_set_dpi_xy", 0x04, 0x05, 0x07, append(args, 0, 0)...)
	return err
}

func (d *Device) maxDPI() int {
	if d.MaxDPI > 0 {
		return d.MaxDPI
	}
	return 16000
}

func (d *Device) checkDPI(x, y int) error {
	if x < 100 || y < 100 || x > d.maxDPI() || y > d.maxDPI() {
		return fmt.Errorf("DPI must be 100–%d", d.maxDPI())
	}
	return nil
}

// DPIStages returns stages and the 1-based active stage.
func (d *Device) DPIStages() ([]DPIStage, int, error) {
	r, err := d.query("misc_get_dpi_stages", 0x04, 0x86, 0x26, varStore)
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
	_, err := d.query("misc_set_dpi_stages", 0x04, 0x06, 0x26, encodeStages(stages, active)...)
	return err
}

// Codes of the original polling-rate command (devices up to 1000 Hz).
var pollCodesV1 = map[int]byte{1000: 0x01, 500: 0x02, 125: 0x08}

func (d *Device) PollRate() (int, error) {
	if d.pollV2() {
		r, err := d.query("misc_get_polling_rate2", 0x00, 0xC0, 0x01)
		if err != nil {
			return 0, err
		}
		return pollFromCode(r.Args()[1]), nil
	}
	r, err := d.query("misc_get_polling_rate", 0x00, 0x85, 0x01)
	if err != nil {
		return 0, err
	}
	for hz, c := range pollCodesV1 {
		if c == r.Args()[0] {
			return hz, nil
		}
	}
	return 0, nil
}

// NearestPollRate is the highest supported rate not above hz (or the lowest).
func (m *Model) NearestPollRate(hz int) int {
	rates := slices.Sorted(slices.Values(m.SupportedPollRates()))
	if len(rates) == 0 {
		return 0
	}
	best := rates[0]
	for _, r := range rates {
		if r <= hz {
			best = r
		}
	}
	return best
}

func (d *Device) SetPollRate(hz int) error {
	if !slices.Contains(d.SupportedPollRates(), hz) {
		return fmt.Errorf("unsupported polling rate %d", hz)
	}
	if !d.pollV2() {
		_, err := d.query("misc_set_polling_rate", 0x00, 0x05, 0x01, pollCodesV1[hz])
		return err
	}
	code := pollCodes[hz]
	if len(d.pollSeq) == 0 {
		_, err := d.query("misc_set_polling_rate2", 0x00, 0x40, 0x02, 0x00, code)
		return err
	}
	// Receivers keep their own copy: target 0x00 is the device, 0x01 the receiver.
	for _, s := range d.pollSeq {
		r := NewReport(parseTx(s.TransactionID), 0x00, 0x40, 0x02, parseTx(s.FixedArgument0), code)
		if _, err := d.send(r); err != nil {
			return err
		}
	}
	return nil
}

// Battery returns 0–100.
func (d *Device) Battery() (int, error) {
	r, err := d.query("misc_get_battery_level", 0x07, 0x80, 0x02)
	if err != nil {
		return 0, err
	}
	return int(r.Args()[1]) * 100 / 255, nil
}

func (d *Device) Charging() (bool, error) {
	r, err := d.query("misc_get_charging_status", 0x07, 0x84, 0x02)
	return err == nil && r.Args()[1] == 1, err
}

// IdleTime is the seconds before the device sleeps (60–900).
func (d *Device) IdleTime() (int, error) {
	r, err := d.query("misc_get_idle_time", 0x07, 0x83, 0x02)
	if err != nil {
		return 0, err
	}
	return be16(r.Args()), nil
}

func (d *Device) SetIdleTime(sec int) error {
	if sec < 60 || sec > 900 {
		return errors.New("idle time must be 60–900 s")
	}
	_, err := d.query("misc_set_idle_time", 0x07, 0x03, 0x02, put16(sec)...)
	return err
}

// LowBattery returns the low-battery warning threshold in percent.
func (d *Device) LowBattery() (int, error) {
	r, err := d.query("misc_get_low_battery_threshold", 0x07, 0x81, 0x01)
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
	_, err := d.query("misc_set_low_battery_threshold", 0x07, 0x01, 0x01, byte((pct*255+50)/100))
	return err
}

// ReceiverLEDs returns what the HyperPolling receiver's left, middle and right
// indicators show: 0 off, 1 battery, 2 connection, 3 polling rate, 4 DPI.
func (d *Device) ReceiverLEDs() ([3]int, error) {
	r, err := d.query("misc_get_hyperpolling_wireless_dongle_multi_indicator_led_modes", 0x07, 0x95, 0x03)
	if err != nil {
		return [3]int{}, err
	}
	a := r.Args()
	return [3]int{int(a[0]), int(a[1]), int(a[2])}, nil
}

func (d *Device) SetReceiverLEDs(m [3]int) error {
	for _, v := range m {
		if v < 0 || v > 4 {
			return errors.New("LED mode must be 0–4")
		}
	}
	_, err := d.query("misc_set_hyperpolling_wireless_dongle_multi_indicator_led_modes", 0x07, 0x15, 0x03, byte(m[0]), byte(m[1]), byte(m[2]))
	return err
}

// Scan finds connected supported devices and picks the HID collection that answers.
func Scan() ([]*Device, error) {
	paths, err := hidPaths()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(paths))
	for p := range paths {
		keys = append(keys, p)
	}
	sort.Strings(keys) // stable order: MI_00 collections first
	// shortcut: one Device per product id; two identical keyboards show as one until IDs include the serial.
	byPID := map[uint16]*Device{}
	for _, p := range keys {
		m := findModel(paths[p])
		if m == nil {
			continue
		}
		d := byPID[m.PID]
		if d == nil {
			d = &Device{Model: m, ID: fmt.Sprintf("1532:%04X", m.PID), txOK: map[string]byte{}}
			byPID[m.PID] = d
		}
		d.candidates = append(d.candidates, &conn{path: p})
	}
	out := make([]*Device, 0, len(byPID))
	var wg sync.WaitGroup
	for _, d := range byPID {
		out = append(out, d)
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Answered = d.probe() == nil
		}()
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return categoryOrder(out[i].Category) < categoryOrder(out[j].Category)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func categoryOrder(c string) int {
	return slices.Index([]string{"mouse", "keyboard", "mouse-mat", "accessory", "monitor"}, c)
}

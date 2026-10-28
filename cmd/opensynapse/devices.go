package main

import (
	"errors"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
	"github.com/JohnsonRan/OpenSynapse/internal/walkplay"
)

// hub owns the connected devices; the UI and the smart switcher share it.
type hub struct {
	mu   sync.Mutex
	devs []*razer.Device
}

var devices hub

func (h *hub) rescan() error {
	found, err := razer.Scan()
	if err != nil {
		return err
	}
	keep := connected(found)
	h.mu.Lock()
	old := h.devs
	h.devs = keep
	h.mu.Unlock()
	for _, d := range old {
		d.Close()
	}
	for _, d := range found {
		if !slices.Contains(keep, d) {
			d.Close()
		}
	}
	return nil
}

// connected keeps one ID per product. A wireless mouse on its cable shows up
// twice: as the wired ID, and as the receiver that can no longer reach it. The
// ID that answers wins, the cable when both do; with no answer, the wireless
// ID stays (the mouse is probably asleep).
func connected(devs []*razer.Device) []*razer.Device {
	best := map[string]*razer.Device{}
	score := func(d *razer.Device) int {
		s := 0
		if d.Answered {
			s += 2
		}
		if d.Wired() == d.Answered { // answering wired, or silent wireless
			s++
		}
		return s
	}
	for _, d := range devs {
		if b, ok := best[d.Family()]; !ok || score(d) > score(b) {
			best[d.Family()] = d
		}
	}
	return slices.DeleteFunc(slices.Clone(devs), func(d *razer.Device) bool { return best[d.Family()] != d })
}

// DevicesChanged tells the window that devices were plugged or unplugged.
var DevicesChanged = mygo.NewEvent[[]DeviceInfo]("devicesChanged")

// watchDevices rescans when the set of attached Razer HID interfaces changes
// (plugging a cable, a receiver, a keyboard). Listing interface paths opens
// nothing, so checking every 2 s is cheap.
func watchDevices() {
	var last string
	started := false
	for ; ; time.Sleep(2 * time.Second) {
		paths, err := razer.PresentPaths()
		if err != nil {
			continue
		}
		audio, _ := walkplay.PresentPaths()
		paths = append(paths, audio...)
		slices.Sort(paths)
		key := strings.Join(paths, "\n")
		if key == last {
			continue
		}
		last = key
		if !started {
			started = true
			continue // main already scanned at startup
		}
		// A device re-enumerates in stages; let it settle before probing it.
		time.Sleep(time.Second)
		if err := devices.rescan(); err != nil {
			log.Println("rescan:", err)
			continue
		}
		if err := dongles.rescan(); err != nil {
			log.Println("rescan audio:", err)
		}
		resetAuto() // a new or switched device gets the current scene and polling rate
		kickBindings()
		watchDPI()
		go refreshTray()
		if w := currentWindow(); w != nil {
			_ = DevicesChanged.Emit(w, Devices{}.List())
		}
	}
}

func (h *hub) list() []*razer.Device {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*razer.Device(nil), h.devs...)
}

func (h *hub) get(id string) (*razer.Device, error) {
	for _, d := range h.list() {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, errors.New("device not connected")
}

// DeviceInfo describes a connected device and the controls it declares.
type DeviceInfo struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Category  string         `json:"category"` // mouse, keyboard, mouse-mat, accessory, monitor
	Family    string         `json:"family"`   // product name without (Wired)/(Wireless)
	Wired     bool           `json:"wired"`
	MaxDPI    int            `json:"maxDpi"`
	PollRates []int          `json:"pollRates"`
	Features  razer.Features `json:"features"`
}

// DeviceState is what the device reports. Fields the device does not declare
// stay zero; Battery is -1 when it cannot be read (asleep, or wired).
type DeviceState struct {
	DPI         razer.DPIStage   `json:"dpi"`
	Stages      []razer.DPIStage `json:"stages"`
	ActiveStage int              `json:"activeStage"`
	PollRate    int              `json:"pollRate"`
	Battery     int              `json:"battery"`
	Charging    bool             `json:"charging"`
	IdleTime    int              `json:"idleTime"`
	LowBattery  int              `json:"lowBattery"`
	// ReceiverLEDs: left/middle/right indicator modes, empty without a receiver.
	ReceiverLEDs []int `json:"receiverLeds"`
	// Brightness is 0–100, -1 when not readable. Lighting is the last effect set
	// here: devices cannot report their effect.
	Brightness int      `json:"brightness"`
	Lighting   Lighting `json:"lighting"`
}

// Devices controls connected Razer devices.
type Devices struct{}

// List returns connected devices.
func (Devices) List() []DeviceInfo {
	out := []DeviceInfo{}
	for _, d := range devices.list() {
		out = append(out, DeviceInfo{d.ID, d.Name, d.Category, d.Family(), d.Wired(), d.MaxDPI, d.SupportedPollRates(), d.Features()})
	}
	return append(out, dongles.infos()...)
}

// Rescan looks for devices again.
func (s Devices) Rescan() ([]DeviceInfo, error) {
	if err := devices.rescan(); err != nil {
		return nil, err
	}
	if err := dongles.rescan(); err != nil {
		return nil, err
	}
	return s.List(), nil
}

// State reads the device's current settings. It fails only when the device
// answers none of its declared reads.
func (Devices) State(id string) (DeviceState, error) {
	d, err := devices.get(id)
	if err != nil {
		return DeviceState{}, err
	}
	f := d.Features()
	st := DeviceState{Battery: -1, Brightness: -1, ReceiverLEDs: []int{}, Stages: []razer.DPIStage{}}
	tried, ok := 0, 0
	var lastErr error
	read := func(on bool, fn func() error) {
		if !on {
			return
		}
		tried++
		if err := fn(); err != nil {
			lastErr = err
		} else {
			ok++
		}
	}
	read(f.DPI, func() (err error) { st.DPI, err = d.DPI(); return })
	read(f.DPIStages, func() (err error) { st.Stages, st.ActiveStage, err = d.DPIStages(); return })
	read(f.PollRate, func() (err error) { st.PollRate, err = d.PollRate(); return })
	read(f.Battery, func() (err error) {
		if st.Battery, err = d.Battery(); err != nil {
			st.Battery = -1
		}
		return
	})
	read(f.Charging, func() (err error) { st.Charging, err = d.Charging(); return })
	read(f.IdleTime, func() (err error) { st.IdleTime, err = d.IdleTime(); return })
	read(f.LowBattery, func() (err error) { st.LowBattery, err = d.LowBattery(); return })
	read(f.ReceiverLEDs, func() error {
		m, err := d.ReceiverLEDs()
		if err == nil {
			st.ReceiverLEDs = m[:]
		}
		return err
	})
	read(f.Lighting != nil && f.Lighting.Brightness, func() (err error) {
		if st.Brightness, err = d.Brightness(); err != nil {
			st.Brightness = -1
		}
		return
	})
	st.Lighting = lightingFor(d)
	if tried > 0 && ok == 0 {
		return st, lastErr
	}
	return st, nil
}

// SetLighting applies an effect (off, static, breathing, spectrum, wave) to the whole device.
func (Devices) SetLighting(id string, l Lighting) error {
	d, err := devices.get(id)
	if err != nil {
		return err
	}
	if err := d.SetLighting(l.Effect, l.Color); err != nil {
		return err
	}
	rememberLighting(d.ID, l)
	return saveConfig()
}

// SetBrightness sets lighting brightness, 0–100.
func (Devices) SetBrightness(id string, pct int) error {
	d, err := devices.get(id)
	if err != nil {
		return err
	}
	return d.SetBrightness(pct)
}

// SetDPIStages writes the sensitivity stages; active is 1-based.
func (Devices) SetDPIStages(id string, stages []razer.DPIStage, active int) error {
	d, err := devices.get(id)
	if err != nil {
		return err
	}
	if err := d.SetDPIStages(stages, active); err != nil {
		return err
	}
	if !d.Features().DPI {
		return nil
	}
	// Make the current sensitivity follow the chosen stage right away.
	return d.SetDPI(stages[active-1].X, stages[active-1].Y)
}

// SetDPI sets the current sensitivity (devices without DPI stages).
func (Devices) SetDPI(id string, x, y int) error {
	d, err := devices.get(id)
	if err != nil {
		return err
	}
	return d.SetDPI(x, y)
}

// SetPollRate sets the polling rate in Hz.
func (Devices) SetPollRate(id string, hz int) error {
	d, err := devices.get(id)
	if err != nil {
		return err
	}
	return d.SetPollRate(hz)
}

// SetIdleTime sets seconds before sleep (60–900).
func (Devices) SetIdleTime(id string, sec int) error {
	d, err := devices.get(id)
	if err != nil {
		return err
	}
	return d.SetIdleTime(sec)
}

// SetReceiverLEDs sets the receiver's left/middle/right indicator modes:
// 0 off, 1 battery, 2 connection, 3 polling rate, 4 DPI.
func (Devices) SetReceiverLEDs(id string, modes []int) error {
	d, err := devices.get(id)
	if err != nil {
		return err
	}
	if len(modes) != 3 {
		return errors.New("need 3 LED modes")
	}
	return d.SetReceiverLEDs([3]int(modes))
}

// SetLowBattery sets the low-battery warning threshold (percent).
func (Devices) SetLowBattery(id string, pct int) error {
	d, err := devices.get(id)
	if err != nil {
		return err
	}
	return d.SetLowBattery(pct)
}

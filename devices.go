package main

import (
	"errors"
	"sync"

	"opensynapse/internal/razer"
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
	h.mu.Lock()
	old := h.devs
	h.devs = found
	h.mu.Unlock()
	for _, d := range old {
		d.Close()
	}
	return nil
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

// DeviceInfo describes a connected device.
type DeviceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MaxDPI    int    `json:"maxDpi"`
	PollRates []int  `json:"pollRates"`
	Wireless  bool   `json:"wireless"`
}

// DeviceState is what the device reports. Battery and charging are -1/false
// when the mouse does not answer them (asleep, or wired).
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
}

// Devices controls connected Razer devices.
type Devices struct{}

// List returns connected devices.
func (Devices) List() []DeviceInfo {
	out := []DeviceInfo{}
	for _, d := range devices.list() {
		out = append(out, DeviceInfo{d.ID, d.Name, d.MaxDPI, d.PollRates, d.Wireless})
	}
	return out
}

// Rescan looks for devices again.
func (s Devices) Rescan() ([]DeviceInfo, error) {
	if err := devices.rescan(); err != nil {
		return nil, err
	}
	return s.List(), nil
}

// State reads the device's current settings.
func (Devices) State(id string) (DeviceState, error) {
	d, err := devices.get(id)
	if err != nil {
		return DeviceState{}, err
	}
	var st DeviceState
	// DPI answers whenever the mouse is reachable; it decides whether the read failed.
	if st.DPI, err = d.DPI(); err != nil {
		return st, err
	}
	st.Stages, st.ActiveStage, _ = d.DPIStages()
	st.PollRate, _ = d.PollRate()
	if st.Battery, err = d.Battery(); err != nil {
		st.Battery = -1
	}
	st.Charging, _ = d.Charging()
	st.IdleTime, _ = d.IdleTime()
	st.LowBattery, _ = d.LowBattery()
	st.ReceiverLEDs = []int{}
	if m, err := d.ReceiverLEDs(); err == nil {
		st.ReceiverLEDs = m[:]
	}
	return st, nil
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
	// Make the current sensitivity follow the chosen stage right away.
	return d.SetDPI(stages[active-1].X, stages[active-1].Y)
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

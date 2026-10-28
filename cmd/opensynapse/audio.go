package main

import (
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
	"github.com/JohnsonRan/OpenSynapse/internal/walkplay"
	"github.com/JohnsonRan/OpenSynapse/internal/winaudio"
)

// USB audio dongles (internal/walkplay): their EQ and DAC options, and the
// format Windows plays to them. They show on the Devices page as "audio".

type audioHub struct {
	mu   sync.Mutex
	devs []*walkplay.Device
}

var dongles audioHub

func (h *audioHub) rescan() error {
	found, err := walkplay.Scan()
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

func (h *audioHub) get(id string) (*walkplay.Device, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, d := range h.devs {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, errors.New("device not connected")
}

// infos describes the dongles for the device list.
func (h *audioHub) infos() []DeviceInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []DeviceInfo{}
	for _, d := range h.devs {
		out = append(out, DeviceInfo{ID: d.ID, Name: d.Name, Category: "audio", Family: d.Name, Features: razer.Features{}})
	}
	return out
}

// Audio controls USB audio dongles.
type Audio struct{}

// AudioState is what the dongle and Windows report. Format is nil when
// Windows has no playback device for the dongle.
type AudioState struct {
	Firmware  string            `json:"firmware"`
	EQ        walkplay.EQ       `json:"eq"`
	DACFilter int               `json:"dacFilter"`
	HighGain  bool              `json:"highGain"`
	WorkMode  int               `json:"workMode"`
	MicGain   int               `json:"micGain"`
	Format    *winaudio.Format  `json:"format"`
	Formats   []winaudio.Format `json:"formats"`
}

// State reads the dongle's settings and its Windows format.
func (Audio) State(id string) (AudioState, error) {
	d, err := dongles.get(id)
	if err != nil {
		return AudioState{}, err
	}
	var st AudioState
	if st.EQ, err = d.EQ(); err != nil {
		return st, err
	}
	// The rest reads fine once the EQ did; a failure leaves its zero value.
	st.Firmware, _ = d.Version()
	st.DACFilter, _ = d.DACFilter()
	st.HighGain, _ = d.HighGain()
	st.WorkMode, _ = d.WorkMode()
	st.MicGain, _ = d.MicGain()
	st.Formats = []winaudio.Format{}
	if ep, _ := winaudio.Endpoint(d.USB); ep != "" {
		if cur, all, err := winaudio.Formats(ep); err == nil {
			st.Format, st.Formats = &cur, all
		}
	}
	return st, nil
}

// SetEQ writes the EQ and saves it in the dongle.
func (Audio) SetEQ(id string, e walkplay.EQ) error {
	d, err := dongles.get(id)
	if err != nil {
		return err
	}
	return d.SetEQ(e)
}

// SetDACFilter picks the DAC's digital filter (1–5).
func (Audio) SetDACFilter(id string, f int) error {
	d, err := dongles.get(id)
	if err != nil {
		return err
	}
	return d.SetDACFilter(f)
}

// SetHighGain switches the amplifier between low and high gain.
func (Audio) SetHighGain(id string, on bool) error {
	d, err := dongles.get(id)
	if err != nil {
		return err
	}
	return d.SetHighGain(on)
}

// SetWorkMode sets the amplifier class: 0 H, 1 AB.
func (Audio) SetWorkMode(id string, m int) error {
	d, err := dongles.get(id)
	if err != nil {
		return err
	}
	return d.SetWorkMode(m)
}

// SetMicGain sets the microphone gain in dB (-15 to 15).
func (Audio) SetMicGain(id string, db int) error {
	d, err := dongles.get(id)
	if err != nil {
		return err
	}
	return d.SetMicGain(db)
}

// SetFormat sets the bit depth and sample rate Windows plays to the dongle.
func (Audio) SetFormat(id string, f winaudio.Format) error {
	d, err := dongles.get(id)
	if err != nil {
		return err
	}
	ep, err := winaudio.Endpoint(d.USB)
	if err != nil {
		return err
	}
	if ep == "" {
		return errors.New(tr("err.noSoundDevice"))
	}
	return winaudio.SetFormat(ep, f)
}

// SoundDevice is a Windows playback device and the formats it can play.
type SoundDevice struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Format  winaudio.Format   `json:"format"`
	Formats []winaudio.Format `json:"formats"`
}

// SoundDevices lists the playback devices with their default format. A device
// whose format cannot be read is left out.
func (Settings) SoundDevices() ([]SoundDevice, error) {
	devs, err := winaudio.Devices()
	if err != nil {
		return nil, err
	}
	out := []SoundDevice{}
	for _, d := range devs {
		if cur, all, err := winaudio.Formats(d.ID); err == nil && len(all) > 0 {
			out = append(out, SoundDevice{d.ID, d.Name, cur, all})
		}
	}
	return out, nil
}

// SetSoundFormat sets the bit depth and sample rate Windows plays to a device.
func (Settings) SetSoundFormat(id string, f winaudio.Format) error {
	return winaudio.SetFormat(id, f)
}

// EQPreset is an EQ the user saved under a name.
type EQPreset struct {
	Name string      `json:"name"`
	EQ   walkplay.EQ `json:"eq"`
}

func (p *EQPreset) normalize() error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return errors.New(tr("err.presetName"))
	}
	return p.EQ.Check()
}

// Presets returns the user's EQ presets.
func (Audio) Presets() []EQPreset {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return slices.Clone(cfg.EQPresets)
}

// SavePreset stores an EQ under a name, replacing a preset of that name.
func (Audio) SavePreset(p EQPreset) error {
	if err := p.normalize(); err != nil {
		return err
	}
	cfgMu.Lock()
	if i := slices.IndexFunc(cfg.EQPresets, func(x EQPreset) bool { return x.Name == p.Name }); i >= 0 {
		cfg.EQPresets[i] = p
	} else {
		cfg.EQPresets = append(cfg.EQPresets, p)
	}
	cfgMu.Unlock()
	return saveConfig()
}

// DeletePreset removes a preset.
func (Audio) DeletePreset(name string) error {
	cfgMu.Lock()
	cfg.EQPresets = slices.DeleteFunc(cfg.EQPresets, func(x EQPreset) bool { return x.Name == name })
	cfgMu.Unlock()
	return saveConfig()
}

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
	"github.com/JohnsonRan/OpenSynapse/internal/walkplay"
	"github.com/JohnsonRan/OpenSynapse/internal/winaudio"
)

// USB audio dongles (internal/walkplay): their EQ and DAC options, and the
// format Windows plays to them. They show on the Devices page as "audio".

type audioHub struct {
	mu     sync.Mutex
	devs   []*walkplay.Device
	scanMu sync.Mutex // one rescan at a time, so an older one cannot forget what a newer one found
}

var dongles audioHub

func (h *audioHub) rescan() error {
	h.scanMu.Lock()
	defer h.scanMu.Unlock()
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
	ids := make([]string, len(found))
	for i, d := range found {
		ids[i] = d.ID
	}
	reconcileSceneAudio(ids)
	return nil
}

// reconcileSceneAudio drops baseline and applied state for dongles that are no longer connected.
func reconcileSceneAudio(present []string) {
	sceneAudio.Lock()
	defer sceneAudio.Unlock()
	changed := false
	for id := range sceneAudio.eqBase {
		if !slices.Contains(present, id) {
			delete(sceneAudio.eqBase, id)
			changed = true
		}
	}
	for id := range sceneAudio.eqOn {
		if !slices.Contains(present, id) {
			delete(sceneAudio.eqOn, id)
			changed = true
		}
	}
	if changed {
		_ = savePendingAudio()
	}
}

func (h *audioHub) list() []*walkplay.Device {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.devs)
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
	sceneAudio.Lock()
	defer sceneAudio.Unlock()
	if err := d.SetEQ(e); err != nil {
		return err
	}
	// The saved EQ is the user's now: leaving a scene keeps it.
	delete(sceneAudio.eqBase, id)
	delete(sceneAudio.eqOn, id)
	return savePendingAudio() // else a restart would put the old EQ back over this one
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
	devs, err := winaudio.Devices(winaudio.Playback)
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
	// Scenes keep a copy of their preset; a changed preset changes them too.
	sc := cfg.Scenes.clone()
	for _, s := range append([]*Scene{&sc.Game, &sc.Desktop}, pointers(sc.Scenes)...) {
		if s.EQ != nil && s.EQ.Preset == "u:"+p.Name {
			s.EQ.EQ = p.EQ
		}
	}
	cfg.Scenes = sc
	cfgMu.Unlock()
	resetAuto()
	return saveConfig()
}

// DeletePreset removes a preset.
func (Audio) DeletePreset(name string) error {
	cfgMu.Lock()
	cfg.EQPresets = slices.DeleteFunc(cfg.EQPresets, func(x EQPreset) bool { return x.Name == name })
	cfgMu.Unlock()
	return saveConfig()
}

func pointers[T any](s []T) []*T {
	out := make([]*T, len(s))
	for i := range s {
		out[i] = &s[i]
	}
	return out
}

// ---------- scenes ----------

// SceneEQ is the EQ a scene plays on audio dongles: a copy of a preset, named
// "b:<built-in key>" or "u:<the user's name>".
type SceneEQ struct {
	Preset string      `json:"preset"`
	EQ     walkplay.EQ `json:"eq"`
}

// sceneAudio remembers what played before scenes changed it, so leaving them
// puts it back.
var sceneAudio = struct {
	sync.Mutex
	quitting bool
	eqBase   map[string]walkplay.EQ // dongle ID → its EQ before scenes
	eqOn     map[string]string      // dongle ID → the scene EQ it plays, as JSON
	out, in  sceneDevice
	volume   sceneVolume
}{eqBase: map[string]walkplay.EQ{}, eqOn: map[string]string{}, volume: sceneVolume{base: map[string]int{}}}

type sceneDevice struct {
	base, on string
	lastErr  string // avoids logging the same error every second
}

// The default-device calls, replaced in tests.
var (
	soundDefault    = winaudio.Default
	setSoundDefault = winaudio.SetDefault
	soundVolume     = winaudio.Volume
	setSoundVolume  = winaudio.SetVolume
)

// apply makes want the default device, or with want "" puts back the one
// from before.
func (sd *sceneDevice) apply(flow winaudio.Flow, want string) {
	switch {
	case want != "" && want != sd.on:
		if sd.base == "" {
			cur, err := soundDefault(flow)
			if err != nil || cur == "" {
				return
			}
			sd.base = cur
			if err := savePendingAudio(); err != nil {
				sd.base = ""
				return
			}
		}
		if err := setSoundDefault(want); err != nil {
			sd.on = "" // it may have switched some roles: try again on the next tick
			msg := err.Error() + ":" + want
			if msg != sd.lastErr {
				log.Println("scene sound device:", err)
				sd.lastErr = msg
			}
			return
		}
		sd.on = want
		sd.lastErr = ""
	case want == "" && sd.base != "":
		if err := setSoundDefault(sd.base); err != nil {
			sd.on = ""
			msg := err.Error() + ":" + sd.base
			if msg != sd.lastErr {
				log.Println("scene sound device:", err)
				sd.lastErr = msg
			}
			return
		}
		sd.base, sd.on = "", ""
		sd.lastErr = ""
	}
}

// applySceneAudio brings the dongles' EQ and the default sound devices to
// the scene s; nil (no scene) puts back what played before. Cheap when
// nothing changes, so it runs every tick.
func applySceneAudio(s *Scene) {
	sceneAudio.Lock()
	defer sceneAudio.Unlock()
	defer savePendingAudio()
	if sceneAudio.quitting && s != nil {
		return
	}
	var eq *SceneEQ
	var out, in string
	if s != nil {
		eq, out, in = s.EQ, s.Output, s.Input
	}
	for _, d := range dongles.list() {
		if eq == nil {
			if b, ok := sceneAudio.eqBase[d.ID]; ok && playEQ(d, b) {
				delete(sceneAudio.eqBase, d.ID)
				delete(sceneAudio.eqOn, d.ID)
			}
			continue
		}
		key, _ := json.Marshal(eq.EQ)
		if sceneAudio.eqOn[d.ID] == string(key) {
			continue
		}
		if _, ok := sceneAudio.eqBase[d.ID]; !ok {
			b, err := d.EQ()
			if err != nil {
				continue // busy or gone: try on the next tick
			}
			sceneAudio.eqBase[d.ID] = b
			if err := savePendingAudio(); err != nil {
				delete(sceneAudio.eqBase, d.ID)
				continue
			}
		}
		if playEQ(d, eq.EQ) {
			sceneAudio.eqOn[d.ID] = string(key)
		}
	}
	sceneAudio.out.apply(winaudio.Playback, out)
	sceneAudio.in.apply(winaudio.Recording, in)
	var vol *int
	if s != nil {
		vol = s.Volume
	}
	sceneAudio.volume.apply(vol)
}

// sceneVolume sets the volume of the playback device in use (after the
// scene switched it) and remembers each device's volume from before.
type sceneVolume struct {
	base map[string]int // device ID → its volume before scenes
	on   string         // "<device>:<volume>" set last; a new device or volume sets again
}

func (v *sceneVolume) apply(want *int) {
	if want == nil {
		for dev, b := range v.base {
			if err := setSoundVolume(dev, b); err == nil {
				delete(v.base, dev)
			}
		}
		v.on = ""
		return
	}
	dev, err := soundDefault(winaudio.Playback)
	if err != nil || dev == "" || v.on == fmt.Sprintf("%s:%d", dev, *want) {
		return
	}
	if _, ok := v.base[dev]; !ok {
		b, err := soundVolume(dev)
		if err != nil {
			return
		}
		v.base[dev] = b
		if err := savePendingAudio(); err != nil {
			delete(v.base, dev)
			return
		}
	}
	if setSoundVolume(dev, *want) == nil {
		v.on = fmt.Sprintf("%s:%d", dev, *want)
	}
}

// restoreAudioOnQuit restores the baseline EQ and sound devices on shutdown,
// retrying until nothing is pending or the retry limit is reached.
func restoreAudioOnQuit() {
	sceneAudio.Lock()
	sceneAudio.quitting = true
	sceneAudio.Unlock()
	for range 5 {
		applySceneAudio(nil)
		sceneAudio.Lock()
		pending := len(sceneAudio.eqBase) > 0 || sceneAudio.out.base != "" || sceneAudio.in.base != "" || len(sceneAudio.volume.base) > 0
		sceneAudio.Unlock()
		if !pending {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// playEQ plays e and reports whether the dongle took it. Right after the
// default sound device changes, a dongle drops what it is sent, so a write
// is read back; one not taken is tried again on the next tick.
// shortcut: retries every second for as long as the dongle keeps dropping writes; give up after a few if that ever happens.
func playEQ(d *walkplay.Device, e walkplay.EQ) bool {
	if err := d.ApplyEQ(e); err != nil {
		log.Println("scene EQ:", err)
		return false
	}
	got, err := d.EQ()
	return err == nil && sameEQ(got, e)
}

// sameEQ compares EQs as the dongle stores them: Q and gain to 1/256.
func sameEQ(a, b walkplay.EQ) bool {
	near := func(x, y float64) bool { return math.Abs(x-y) < 0.01 }
	return a.Preamp == b.Preamp && slices.EqualFunc(a.Bands, b.Bands, func(x, y walkplay.Band) bool {
		return x.Freq == y.Freq && x.Type == y.Type && near(x.Q, y.Q) && near(x.Gain, y.Gain)
	})
}

// AudioDevices are the playback and recording devices and the defaults.
type AudioDevices struct {
	Playback  []winaudio.Device `json:"playback"`
	Recording []winaudio.Device `json:"recording"`
	Output    string            `json:"output"` // default playback device ID
	Input     string            `json:"input"`
}

// AudioDevices lists the sound devices and which ones are the defaults.
func (Settings) AudioDevices() (AudioDevices, error) {
	var a AudioDevices
	var err error
	if a.Playback, err = winaudio.Devices(winaudio.Playback); err != nil {
		return a, err
	}
	if a.Recording, err = winaudio.Devices(winaudio.Recording); err != nil {
		return a, err
	}
	a.Output, _ = winaudio.Default(winaudio.Playback)
	a.Input, _ = winaudio.Default(winaudio.Recording)
	if a.Playback == nil {
		a.Playback = []winaudio.Device{}
	}
	if a.Recording == nil {
		a.Recording = []winaudio.Device{}
	}
	return a, nil
}

// SetDefaultDevice makes a playback or recording device the default.
func (Settings) SetDefaultDevice(id string) error {
	return winaudio.SetDefault(id)
}

// pendingAudio is what scenes changed and must put back, kept in a file so
// that a crash, a forced end or a lost quit still restores it at the next start.
type pendingAudio struct {
	EQ     map[string]walkplay.EQ `json:"eq,omitempty"`
	Output string                 `json:"output,omitempty"`
	Input  string                 `json:"input,omitempty"`
	Volume map[string]int         `json:"volume,omitempty"`
}

var (
	lastPending    []byte // what the file holds, so it is written only on a change
	lastPendingErr string
)

func pendingPath() string { return filepath.Join(filepath.Dir(configPath()), "scene-audio.json") }

// savePendingAudio writes the baselines, or removes the file when none are
// left; sceneAudio is locked.
func savePendingAudio() error {
	p := pendingAudio{EQ: sceneAudio.eqBase, Output: sceneAudio.out.base, Input: sceneAudio.in.base, Volume: sceneAudio.volume.base}
	b, _ := json.Marshal(p)
	if string(b) == string(lastPending) {
		return nil
	}
	var err error
	if len(p.EQ) == 0 && p.Output == "" && p.Input == "" && len(p.Volume) == 0 {
		if err = os.Remove(pendingPath()); errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	} else if err = os.WriteFile(pendingPath()+".tmp", b, 0o644); err == nil {
		err = os.Rename(pendingPath()+".tmp", pendingPath())
	}
	if err != nil {
		msg := err.Error()
		if msg != lastPendingErr {
			log.Println("scene audio state:", err)
			lastPendingErr = msg
		}
		return err
	}
	lastPendingErr = ""
	lastPending = b
	return nil
}

// loadPendingAudio takes back what a previous run left to restore; the first
// tick puts it back unless a scene that sets it is active.
func loadPendingAudio() {
	b, err := os.ReadFile(pendingPath())
	if err != nil {
		return
	}
	var p pendingAudio
	if json.Unmarshal(b, &p) != nil {
		return
	}
	sceneAudio.Lock()
	defer sceneAudio.Unlock()
	for id, e := range p.EQ {
		if e.Check() == nil {
			sceneAudio.eqBase[id] = e
			sceneAudio.eqOn[id] = "?" // plays something else: the next tick decides
		}
	}
	sceneAudio.out.base, sceneAudio.in.base = p.Output, p.Input
	for dev, v := range p.Volume {
		sceneAudio.volume.base[dev] = v
	}
	lastPending = b
}

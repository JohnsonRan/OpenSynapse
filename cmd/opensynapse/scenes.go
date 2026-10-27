package main

import (
	"errors"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

// gpuGameThreshold is the 3D-engine load (percent) that marks a fullscreen
// window as a game. Fullscreen video and slides stay well below it.
const gpuGameThreshold = 15

// notGames are fullscreen regulars that can still load the GPU (browser
// video, WebGL, players). Listing one by name still works.
var notGames = map[string]bool{
	"chrome.exe": true, "msedge.exe": true, "firefox.exe": true, "vivaldi.exe": true,
	"opera.exe": true, "brave.exe": true, "vlc.exe": true, "potplayermini64.exe": true,
	"mpv.exe": true, "mpc-hc64.exe": true, "powerpnt.exe": true, "explorer.exe": true,
}

const (
	reasonDefault = "default"
	reasonRule    = "rule"
	reasonGame    = "game"
)

// Scene indexes besides custom scenes (0…).
const (
	desktop   = -1
	gameScene = -2
)

// isGame: fullscreen/borderless with GPU load, or a process already recognized.
func isGame(exe string, fullscreen bool, gpu float64, knownGame bool) bool {
	return !notGames[exe] && (knownGame || fullscreen && gpu >= gpuGameThreshold)
}

// decideScene: a custom scene listing the program, then the game scene
// (detected games and the programs it lists), then the desktop.
func decideScene(c ScenesConfig, exe string, game bool) (int, string) {
	for i, s := range c.Scenes {
		if slices.Contains(s.Apps, exe) {
			return i, reasonRule
		}
	}
	if slices.Contains(c.Game.Apps, exe) {
		return gameScene, reasonRule
	}
	if game {
		return gameScene, reasonGame
	}
	return desktop, reasonDefault
}

func (c ScenesConfig) scene(idx int) Scene {
	switch idx {
	case desktop:
		return c.Desktop
	case gameScene:
		return c.Game
	}
	return c.Scenes[idx]
}

// decideRate: an exception for the program, then the game rate, then the
// normal rate. 0 means each mouse's highest rate.
func decideRate(c SmartPoll, exe string, game bool) (int, string) {
	for _, r := range c.Exceptions {
		if r.Exe == exe {
			return r.Rate, reasonRule
		}
	}
	if game {
		return c.GameRate, reasonGame
	}
	return c.NormalRate, reasonDefault
}

// debouncer settles on a target only after it was seen twice in a row, so
// alt-tabbing through a game does not flip devices back and forth.
type debouncer struct {
	pending, current int
	settled          bool
}

func newDebouncer() debouncer { return debouncer{pending: -1 << 30} } // nothing seen yet

func (d *debouncer) observe(target int) (changed bool) {
	switch {
	case d.settled && target == d.current:
		d.pending = target
		return false
	case target != d.pending:
		d.pending = target
		return false
	}
	d.current, d.settled = target, true
	return true
}

// applier brings each device to a target once, retrying a device that did not
// answer (asleep) every 10 s instead of hammering it every tick.
type applier struct {
	name    string
	done    map[string]int // device ID -> target applied
	retryAt map[string]time.Time
}

func (a *applier) reset() { a.done, a.retryAt = map[string]int{}, map[string]time.Time{} }

func (a *applier) run(target int, devs []*razer.Device, apply func(*razer.Device) error) {
	for _, d := range devs {
		if v, ok := a.done[d.ID]; (ok && v == target) || time.Now().Before(a.retryAt[d.ID]) {
			continue
		}
		if err := apply(d); err != nil && !errors.Is(err, razer.ErrUnsupported) {
			log.Printf("%s: %s: %v", a.name, d.ID, err)
			a.retryAt[d.ID] = time.Now().Add(10 * time.Second)
			continue
		}
		a.done[d.ID] = target
	}
}

// switcher is one debounced target plus its per-device applier.
type switcher struct {
	deb debouncer
	app applier
}

func newSwitcher(name string) switcher {
	s := switcher{deb: newDebouncer(), app: applier{name: name}}
	s.app.reset()
	return s
}

func (s *switcher) step(target int, devs []*razer.Device, apply func(*razer.Device) error) {
	if s.deb.observe(target) {
		s.app.reset() // a new target: every device needs it
	}
	if s.deb.settled {
		s.app.run(s.deb.current, devs, apply)
	}
}

// applyScene writes the settings the scene sets; unset ones and controls the
// device lacks are left alone.
func applyScene(d *razer.Device, s Scene) error {
	f := d.Features()
	var errs []error
	if s.DPI > 0 && f.DPI && d.Category == "mouse" {
		v := min(s.DPI, max(d.MaxDPI, 100))
		errs = append(errs, d.SetDPI(v, v))
	}
	if l := f.Lighting; l != nil {
		if s.Lighting != nil && slices.Contains(l.Effects, s.Lighting.Effect) {
			if err := d.SetLighting(s.Lighting.Effect, s.Lighting.Color); err == nil {
				rememberLighting(d.ID, *s.Lighting)
			} else {
				errs = append(errs, err)
			}
		}
		if s.Brightness != nil && l.Brightness {
			errs = append(errs, d.SetBrightness(*s.Brightness))
		}
	}
	errs = slices.DeleteFunc(errs, func(e error) bool { return e == nil || errors.Is(e, razer.ErrUnsupported) })
	return errors.Join(errs...)
}

// applyRate sets a mouse's polling rate: 0 is its highest, and a rate above
// what it supports becomes its highest. Other devices keep theirs.
func applyRate(d *razer.Device, hz int) error {
	if d.Category != "mouse" || !d.Features().PollRate {
		return nil
	}
	if hz == 0 {
		hz = 1 << 30
	}
	return d.SetPollRate(d.NearestPollRate(hz))
}

// AutoStatus is what the switchers see right now.
type AutoStatus struct {
	Foreground string  `json:"foreground"`
	Fullscreen bool    `json:"fullscreen"`
	GPU        float64 `json:"gpu"`  // 3D load of the foreground app, -1 if not measured
	Game       bool    `json:"game"` // recognized as a game
	Scene      string  `json:"scene"`
	SceneWhy   string  `json:"sceneWhy"` // default, rule or game
	Rate       int     `json:"rate"`     // target polling rate, 0 = highest, -1 = off
	RateWhy    string  `json:"rateWhy"`
}

var (
	autoMu     sync.Mutex
	autoStatus AutoStatus
	autoReset  = make(chan struct{}, 1)
)

func resetAuto() {
	select {
	case autoReset <- struct{}{}:
	default:
	}
}

// runAuto drives smart polling and scenes from the foreground program.
func runAuto() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var gpu gpuMeter
	scenes, rates := newSwitcher("scene"), newSwitcher("polling rate")
	// Processes recognized as games stay games while they run, so a loading
	// screen or a windowed menu does not count as leaving the game.
	games := map[uint32]string{}
	for {
		select {
		case <-tick.C:
		case <-autoReset:
			// config changed: re-evaluate from scratch
			scenes, rates, games = newSwitcher("scene"), newSwitcher("polling rate"), map[uint32]string{}
		}
		cfgMu.Lock()
		sc, sp := cfg.Scenes, cfg.Smart
		cfgMu.Unlock()
		if !sc.Enabled {
			scenes = newSwitcher("scene")
		}
		if !sp.Enabled {
			rates = newSwitcher("polling rate")
		}
		if !sc.Enabled && !sp.Enabled {
			gpu.close()
			setTrayStatus(AutoStatus{Rate: -1})
			continue
		}
		f := getForeground()
		if f.exe == "" {
			continue // lock screen, UAC prompt, protected process: keep the current settings
		}
		known := games[f.pid] == f.exe
		needGame := sp.Enabled || sc.Enabled
		load := -1.0
		if needGame && f.fullscreen && !known && !notGames[f.exe] {
			load = gpu.usage(f.pid) // measured only for an unrecognized fullscreen app
		} else {
			gpu.close()
		}
		game := needGame && isGame(f.exe, f.fullscreen, load, known)
		if game && !known {
			if len(games) > 64 {
				clear(games) // shortcut: forget old games wholesale; a pid reused by the same exe is the only cost
			}
			games[f.pid] = f.exe
		}
		devs := devices.list()
		st := AutoStatus{Foreground: f.exe, Fullscreen: f.fullscreen, GPU: load, Game: game, Rate: -1}
		if sc.Enabled {
			idx, why := decideScene(sc, f.exe, game)
			s := sc.scene(idx)
			name := s.Name
			switch idx {
			case gameScene:
				name = tr("scene.game")
			case desktop:
				name = tr("scene.desktop")
			}
			scenes.step(idx, devs, func(d *razer.Device) error { return applyScene(d, s) })
			st.Scene, st.SceneWhy = name, why
		}
		if sp.Enabled {
			hz, why := decideRate(sp, f.exe, game)
			rates.step(hz, devs, func(d *razer.Device) error { return applyRate(d, hz) })
			st.Rate, st.RateWhy = hz, why
		}
		autoMu.Lock()
		autoStatus = st
		autoMu.Unlock()
		setTrayStatus(st)
	}
}

// Scenes switches device settings by the foreground program.
type Scenes struct{}

// Config returns the scenes setup.
func (Scenes) Config() ScenesConfig {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return cfg.Scenes.clone()
}

// SetConfig validates, saves and applies a new scenes setup.
func (Scenes) SetConfig(c ScenesConfig) error {
	if err := c.normalize(); err != nil {
		return err
	}
	cfgMu.Lock()
	cfg.Scenes = c
	cfgMu.Unlock()
	resetAuto()
	return saveConfig()
}

// Smart returns the smart polling-rate setup.
func (Scenes) Smart() SmartPoll {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	c := cfg.Smart
	c.Exceptions = slices.Clone(c.Exceptions)
	return c
}

// SetSmart validates, saves and applies a new smart polling-rate setup.
func (Scenes) SetSmart(c SmartPoll) error {
	if err := c.normalize(); err != nil {
		return err
	}
	cfgMu.Lock()
	cfg.Smart = c
	cfgMu.Unlock()
	resetAuto()
	return saveConfig()
}

// Status returns the foreground app, the active scene and the target rate.
func (Scenes) Status() AutoStatus {
	autoMu.Lock()
	defer autoMu.Unlock()
	return autoStatus
}

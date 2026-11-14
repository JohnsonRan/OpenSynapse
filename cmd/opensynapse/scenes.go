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

// gameSeconds is how many consecutive one-second samples a fullscreen app must
// look like a game before it counts as one. A player's start-up, a seek or a
// transition loads the GPU for a moment; a game keeps it loaded.
const gameSeconds = 5

// notGames are fullscreen regulars that can still load the GPU (browser
// video, WebGL, players). Listing one by name still works.
var notGames = map[string]bool{
	"chrome.exe": true, "msedge.exe": true, "firefox.exe": true, "vivaldi.exe": true,
	"opera.exe": true, "brave.exe": true, "vlc.exe": true, "potplayermini64.exe": true,
	"potplayermini.exe": true, "potplayer.exe": true, "potplayer64.exe": true,
	"mpv.exe": true, "mpc-hc64.exe": true, "mpc-hc.exe": true, "mpc-be64.exe": true,
	"mpc-be.exe": true, "wmplayer.exe": true, "powerpnt.exe": true, "explorer.exe": true,
}

// looksLikeGame: one sample of a fullscreen/borderless app with 3D load that
// is not decoding video. Players render through the 3D engine too, but only
// they keep the video-decode engine busy.
func looksLikeGame(exe string, fullscreen bool, load3D, decode float64) bool {
	return !notGames[exe] && fullscreen && load3D >= gpuGameThreshold && decode <= 0
}

// gameStreak counts consecutive game-like samples of one process.
type gameStreak struct {
	pid uint32
	n   int
}

// observe records a sample and reports whether the process has looked like a
// game for gameSeconds in a row.
func (s *gameStreak) observe(pid uint32, like bool) bool {
	if pid != s.pid || !like {
		*s = gameStreak{pid: pid}
	}
	if like {
		s.n++
	}
	return s.n >= gameSeconds
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
		err := apply(d)
		if errors.Is(err, razer.ErrClosed) {
			continue // a rescan replaced the device during this round: the next one has the new list
		}
		if err != nil && !errors.Is(err, razer.ErrUnsupported) {
			log.Printf("%s: %s: %v", a.name, d.ID, err)
			a.retryAt[d.ID] = time.Now().Add(10 * time.Second)
			continue
		}
		deviceUpdated(d.ID)
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

// auto is what runAuto keeps between its rounds.
type auto struct {
	gpu           gpuMeter
	streak        gameStreak
	scenes, rates switcher
	// Processes recognized as games stay games while they run, so a loading
	// screen or a windowed menu does not count as leaving the game.
	games      map[uint32]string
	shownScene int // the scene last applied, to notice a change but not the first one
}

func newAuto() *auto {
	a := &auto{games: map[uint32]string{}, shownScene: noScene}
	a.reset()
	return a
}

// reset makes the next round re-evaluate from scratch, after a config change.
// Recognized games stay: they name processes, not settings, and must not drop
// to the desktop for a few seconds.
func (a *auto) reset() {
	a.scenes, a.rates = newSwitcher("scene"), newSwitcher("polling rate")
}

// autoEvery is how often runAuto looks at the foreground program: a scene
// follows a program switch within two looks (see debouncer), and a game is
// recognized from gameSeconds looks in a row.
const autoEvery = time.Second

// runAuto drives smart polling and scenes from the foreground program. With
// both off and nothing left to put back it sleeps until the config changes:
// resetAuto wakes it.
func runAuto() {
	tick := time.NewTicker(autoEvery)
	defer tick.Stop()
	a := newAuto()
	idle := false
	for {
		select {
		case <-tick.C:
		case <-autoReset:
			a.reset()
		}
		was := idle
		idle = false // a round that panics is tried again
		guard("scenes and smart polling", func() { idle = a.round() })
		switch {
		case idle && !was:
			tick.Stop()
		case was && !idle:
			tick.Reset(autoEvery)
		}
	}
}

// round looks at the foreground program once and brings the devices to its
// scene and polling rate. It reports whether there is nothing to do until the
// config changes.
func (a *auto) round() (idle bool) {
	cfgMu.Lock()
	sc, sp := cfg.Scenes, cfg.Smart
	cfgMu.Unlock()
	if !sc.Enabled {
		a.scenes = newSwitcher("scene")
	}
	if !sp.Enabled {
		a.rates = newSwitcher("polling rate")
	}
	if !sc.Enabled {
		applySceneAudio(nil)
		applySceneKeyboards(nil)
	}
	if !sc.Enabled && !sp.Enabled {
		a.gpu.close()
		setTrayStatus(AutoStatus{Rate: -1})
		// A device that was busy or unplugged still has a scene's sound or
		// lighting: the next rounds try it again.
		return !sceneAudioPending() && !sceneKeyboards.pending()
	}
	f := getForeground(wantTitle(sc))
	if f.exe == "" {
		return false // lock screen, UAC prompt, protected process: keep the current settings
	}
	known := a.games[f.pid] == f.exe
	needGame := sp.Enabled || sc.Enabled
	load, decode := -1.0, -1.0
	if needGame && f.fullscreen && !known && !notGames[f.exe] {
		load, decode = a.gpu.usage(f.pid) // measured only for an unrecognized fullscreen app
	} else {
		a.gpu.close()
	}
	game := needGame && (known || a.streak.observe(f.pid, looksLikeGame(f.exe, f.fullscreen, load, decode)))
	if game && !known {
		if len(a.games) > 64 {
			clear(a.games) // shortcut: forget old games wholesale; a pid reused by the same exe is the only cost
		}
		a.games[f.pid] = f.exe
	}
	devs := devices.list()
	st := AutoStatus{Foreground: f.exe, Fullscreen: f.fullscreen, GPU: load, Game: game, Rate: -1}
	if sc.Enabled {
		idx, why := decideScene(sc, f.exe, f.title, game)
		s := sc.scene(idx)
		name := s.Name
		switch idx {
		case gameScene:
			name = tr("scene.game")
		case desktop:
			name = tr("scene.desktop")
		}
		a.scenes.step(idx, devs, func(d *razer.Device) error { return applyScene(d, s) })
		if a.scenes.deb.settled && a.scenes.deb.current == idx {
			if a.shownScene != idx && a.shownScene != noScene {
				flashKeyboards(noticeScene)
			}
			a.shownScene = idx
			applySceneAudio(&s)
			applySceneKeyboards(&s)
		}
		st.Scene, st.SceneWhy = name, why
	}
	if sp.Enabled {
		hz, why := decideRate(sp, f.exe, game)
		a.rates.step(hz, devs, func(d *razer.Device) error { return applyRate(d, hz) })
		st.Rate, st.RateWhy = hz, why
	}
	autoMu.Lock()
	autoStatus = st
	autoMu.Unlock()
	setTrayStatus(st)
	return false
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

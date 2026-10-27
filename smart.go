package main

import (
	"log"
	"sync"
	"time"
)

// gpuGameThreshold is the 3D-engine load (percent) that marks a fullscreen
// window as a game. Fullscreen video and slides stay well below it.
const gpuGameThreshold = 15

// notGames are fullscreen regulars that can still load the GPU (browser
// video, WebGL, players). A manual rule still overrides this.
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

// decide picks the rate for the foreground app: a manual rule first, then
// auto-detection (fullscreen/borderless with GPU load, or a process already
// recognized as a game), then the default.
func decide(c SmartConfig, exe string, fullscreen bool, gpu float64, knownGame bool) (int, string) {
	for _, r := range c.Rules {
		if r.Exe == exe {
			return r.Rate, reasonRule
		}
	}
	if c.AutoDetect && !notGames[exe] && (knownGame || fullscreen && gpu >= gpuGameThreshold) {
		return c.AutoRate, reasonGame
	}
	return c.DefaultRate, reasonDefault
}

// switcher applies a rate only after the same target was seen twice in a row,
// so alt-tabbing through a game does not flip the mouse back and forth.
type switcher struct {
	pending, applied int
	apply            func(int) bool
}

func (s *switcher) observe(target int) {
	if target == s.applied {
		s.pending = 0
		return
	}
	if target != s.pending {
		s.pending = target
		return
	}
	if s.apply(target) {
		s.applied, s.pending = target, 0
	}
}

// SmartStatus is what the switcher sees right now.
type SmartStatus struct {
	Foreground string  `json:"foreground"`
	Fullscreen bool    `json:"fullscreen"`
	GPU        float64 `json:"gpu"`    // 3D load of the foreground app, -1 if not measured
	Reason     string  `json:"reason"` // default, rule or game
	Applied    int     `json:"applied"`
}

var (
	smartMu     sync.Mutex
	smartStatus SmartStatus
	smartReset  = make(chan struct{}, 1)
)

func runSmart() {
	sw := &switcher{apply: applyRateAll}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var gpu gpuMeter
	// Processes recognized as games stay games while they run, so a loading
	// screen or a windowed menu does not drop the rate.
	games := map[uint32]string{}
	for {
		select {
		case <-tick.C:
		case <-smartReset:
			sw.applied, sw.pending = 0, 0 // config changed: re-evaluate from scratch
		}
		cfgMu.Lock()
		c := cfg.Smart
		cfgMu.Unlock()
		if !c.Enabled {
			sw.applied = 0
			gpu.close()
			continue
		}
		f := getForeground()
		if f.exe == "" {
			continue // lock screen, UAC prompt, protected process: keep the current rate
		}
		known := games[f.pid] == f.exe
		load := -1.0
		if _, why := decide(c, f.exe, f.fullscreen, 0, known); why == reasonDefault && c.AutoDetect && f.fullscreen && !notGames[f.exe] {
			load = gpu.usage(f.pid) // measured only for an unrecognized fullscreen app
		} else {
			gpu.close()
		}
		rate, why := decide(c, f.exe, f.fullscreen, load, known)
		if why == reasonGame && !known {
			if len(games) > 64 {
				clear(games) // shortcut: forget old games wholesale; a pid reused by the same exe is the only cost
			}
			games[f.pid] = f.exe
		}
		sw.observe(rate)
		smartMu.Lock()
		smartStatus = SmartStatus{f.exe, f.fullscreen, load, why, sw.applied}
		smartMu.Unlock()
	}
}

func applyRateAll(hz int) bool {
	devs := devices.list()
	if len(devs) == 0 {
		_ = devices.rescan() // the mouse may have been plugged in since
		devs = devices.list()
	}
	ok := len(devs) > 0
	for _, d := range devs {
		if err := d.SetPollRate(hz); err != nil {
			log.Printf("smart: %s %d Hz: %v", d.ID, hz, err)
			ok = false // retried on the next tick
		} else {
			log.Printf("smart: %s -> %d Hz", d.ID, hz)
		}
	}
	return ok
}

// Smart configures smart polling-rate switching.
type Smart struct{}

// Config returns the current setup.
func (Smart) Config() SmartConfig {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	c := cfg.Smart
	c.Rules = append([]Rule(nil), c.Rules...)
	return c
}

// SetConfig validates, saves and applies a new setup.
func (Smart) SetConfig(c SmartConfig) error {
	if err := c.normalize(); err != nil {
		return err
	}
	cfgMu.Lock()
	cfg.Smart = c
	cfgMu.Unlock()
	select {
	case smartReset <- struct{}{}:
	default:
	}
	return saveConfig()
}

// Status returns the foreground app and the rate last applied (0 = none yet).
func (Smart) Status() SmartStatus {
	smartMu.Lock()
	defer smartMu.Unlock()
	return smartStatus
}

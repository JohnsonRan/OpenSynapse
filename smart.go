package main

import (
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// foregroundExe returns the lower-case file name of the foreground app, or "".
func foregroundExe() string {
	hwnd := windows.GetForegroundWindow()
	if hwnd == 0 {
		return ""
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil || pid == 0 {
		return ""
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return strings.ToLower(filepath.Base(windows.UTF16ToString(buf[:n])))
}

// targetRate picks the rate for the foreground app.
func targetRate(c SmartConfig, exe string) int {
	for _, r := range c.Rules {
		if r.Exe == exe {
			return r.Rate
		}
	}
	return c.DefaultRate
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
	Foreground string `json:"foreground"`
	Applied    int    `json:"applied"`
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
			continue
		}
		exe := foregroundExe()
		if exe == "" {
			continue // lock screen, UAC prompt, protected process: keep the current rate
		}
		sw.observe(targetRate(c, exe))
		smartMu.Lock()
		smartStatus = SmartStatus{exe, sw.applied}
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

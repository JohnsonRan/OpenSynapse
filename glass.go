package main

import (
	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

// The glass theme shows the window's material, MyGo's VibrancyGlass, behind a
// translucent page: Acrylic on Windows 11, which blurs what is behind the
// window. (MyGo's glass plugin draws Liquid Glass in native UI only, not in
// a page.) Windows shows a material only in a window created with one, so a
// window is always created with it where the system has it, and the theme
// turns it off and on.

// glassSupported: Windows 11 22H2 (build 22621) and later have the material.
var glassSupported = windows.RtlGetVersion().BuildNumber >= 22621

func glassOn() bool {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return glassSupported && cfg.Glass
}

func glassVibrancy(on bool) mygo.Vibrancy {
	if on {
		return mygo.VibrancyGlass
	}
	return mygo.VibrancyNone
}

// GlassInfo is the theme switch: whether the system can show it, and whether it is on.
type GlassInfo struct {
	Supported bool `json:"supported"`
	On        bool `json:"on"`
}

// Glass returns the glass theme setting.
func (Settings) Glass() GlassInfo { return GlassInfo{glassSupported, glassOn()} }

// SetGlass turns the glass theme on or off; the page restyles itself.
func (Settings) SetGlass(on bool) error {
	cfgMu.Lock()
	cfg.Glass = on
	cfgMu.Unlock()
	if w := currentWindow(); w != nil && glassSupported {
		w.SetVibrancy(glassVibrancy(on))
	}
	return saveConfig()
}

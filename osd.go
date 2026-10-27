package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

// The DPI display: when a mouse's DPI button changes its sensitivity, a small
// window shows the new value near the bottom of the screen for a moment. It
// is MyGo native UI, so it needs no webview, and it never takes the focus.

const (
	osdWidth, osdHeight = 220, 64
	osdShown            = 1200 * time.Millisecond
)

var osd struct {
	win  *mygo.Window // main thread only
	text string
	hide *time.Timer
}

func osdOn() bool {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return cfg.DPIDisplay
}

func osdView(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	root := ui.Row(c).Fill().Center()
	if !c.Vibrancy() { // without the glass theme's material, a panel of its own
		root.Radius(14).Background(ui.RGBA(20, 20, 20, 0.94)).Border(1, ui.RGBA(68, 214, 44, 0.5))
	}
	root.Children(func() {
		ui.Text(c, osd.text).FontSize(26).Bold().TextColor(ui.Hex("#44d62c"))
	})
}

// showDPI shows the sensitivity a mouse switched to.
func showDPI(s razer.DPIStage) {
	text := fmt.Sprintf("%d DPI", s.X)
	if s.X != s.Y {
		text = fmt.Sprintf("%d × %d DPI", s.X, s.Y)
	}
	mygo.RunOnMain(func() {
		osd.text = text
		if osd.win == nil {
			glass := glassOn()
			osd.win = mygo.NewWindow(mygo.WindowOptions{
				Title: "OpenSynapse DPI", Width: osdWidth, Height: osdHeight,
				Frameless: true, Transparent: true, AlwaysOnTop: true, SkipTaskbar: true,
				DisableResize: true, Hidden: true, CornerRadius: 14,
				Vibrancy: glassVibrancy(glass),
				Content:  ui.View(osdView),
			})
			osd.win.OnClosed(func() { osd.win = nil })
		}
		wa := mygo.Screen.PrimaryDisplay().WorkArea
		osd.win.SetPosition(wa.X+(wa.Width-osdWidth)/2, wa.Y+wa.Height-osdHeight-96)
		osd.win.Invalidate()
		osd.win.ShowInactive()
		if osd.hide != nil {
			osd.hide.Stop()
		}
		osd.hide = time.AfterFunc(osdShown, func() {
			mygo.RunOnMain(func() {
				if osd.win != nil {
					osd.win.Hide()
				}
			})
		})
	})
}

var dpiWatch struct {
	sync.Mutex
	stop chan struct{}
}

// watchDPI listens to the connected mice again, after a scan.
func watchDPI() {
	dpiWatch.Lock()
	defer dpiWatch.Unlock()
	if dpiWatch.stop != nil {
		close(dpiWatch.stop)
	}
	dpiWatch.stop = make(chan struct{})
	for _, d := range devices.list() {
		if d.Category == "mouse" {
			razer.WatchDPI(d.PID, dpiWatch.stop, func(s razer.DPIStage) {
				if osdOn() {
					showDPI(s)
				}
			})
		}
	}
}

// DPIDisplay reports whether DPI changes are shown on the screen.
func (Settings) DPIDisplay() bool { return osdOn() }

// SetDPIDisplay turns the on-screen DPI display on or off.
func (Settings) SetDPIDisplay(on bool) error {
	cfgMu.Lock()
	cfg.DPIDisplay = on
	cfgMu.Unlock()
	return saveConfig()
}

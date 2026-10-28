// OpenSynapse is a lightweight replacement for Razer Synapse.
//
// The resident process is Go only: HID, the smart polling-rate switcher and a
// tray icon. The WebView2 settings window exists only while it is open, so the
// app costs a few MB in the background instead of hundreds.
package main

import (
	_ "embed"
	"log"
	"os"
	"slices"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"golang.org/x/sys/windows/registry"
)

// Images live in the executable, not in a resources folder next to it.
var (
	//go:embed assets/tray.png
	trayIcon []byte
	//go:embed assets/icon.png
	appIcon []byte
)

var (
	window   *mygo.Window // main thread only
	windowMu sync.Mutex
	shownWin *mygo.Window // the same window, for other goroutines
)

func currentWindow() *mygo.Window {
	windowMu.Lock()
	defer windowMu.Unlock()
	return shownWin
}

func setWindow(w *mygo.Window) {
	window = w
	windowMu.Lock()
	shownWin = w
	windowMu.Unlock()
}

// OpenTarget tells an open window to show a device (its ID) or "scenes".
var OpenTarget = mygo.NewEvent[string]("openTarget")

var (
	targetMu      sync.Mutex
	pendingTarget string // for a window that is still loading
)

// openTarget opens the window on a device page or the scenes page.
func openTarget(target string) {
	if window != nil {
		openWindow()
		_ = OpenTarget.Emit(window, target)
		return
	}
	targetMu.Lock()
	pendingTarget = target
	targetMu.Unlock()
	openWindow()
}

// TakeTarget returns, once, the page a tray click asked a new window to show.
func (Devices) TakeTarget() string {
	targetMu.Lock()
	defer targetMu.Unlock()
	t := pendingTarget
	pendingTarget = ""
	return t
}

func openWindow() {
	if window != nil {
		window.Restore()
		window.Focus()
		return
	}
	size := windowSize(mygo.Screen.PrimaryDisplay().WorkArea)
	w := mygo.NewWindow(mygo.WindowOptions{
		Title:           "OpenSynapse",
		Width:           size.w,
		Height:          size.h,
		MinWidth:        size.minW,
		MinHeight:       size.minH,
		BackgroundColor: "#0a0a0a",
		// No title bar; Windows keeps its own min/max/close (and Snap) over the page.
		TitleBarStyle:  mygo.TitleBarHidden,
		TitleBarHeight: 40,
		StateKey:       "main",
		URL:            "/",
		// The glass theme's material; the page is opaque while it is off.
		Transparent: glassSupported,
		Vibrancy:    glassVibrancy(glassSupported),
	})
	if glassSupported && !glassOn() {
		w.SetVibrancy(mygo.VibrancyNone)
	}
	// Closing destroys the webview and its browser processes; the tray keeps running.
	w.OnClosed(func() { setWindow(nil) })
	setWindow(w)
}

type winSize struct{ w, h, minW, minH int }

// windowSize fits the first window to the screen, in DIPs so display scaling
// is already counted: about 60% x 75% of the work area, no smaller than the
// layout needs and no larger than it uses, and never beyond the screen.
// A size the user chose is remembered (StateKey) and wins over this.
func windowSize(work mygo.Rectangle) winSize {
	clamp := func(v, lo, hi int) int { return max(lo, min(v, hi)) }
	s := winSize{minW: min(960, work.Width), minH: min(620, work.Height)}
	s.w = min(clamp(work.Width*60/100, 960, 1320), work.Width)
	s.h = min(clamp(work.Height*75/100, 620, 900), work.Height)
	return s
}

// trayMenu: device batteries and the active scene/rate on top (read only),
// then the window, the two automatic features, and quit.
func trayMenu() *mygo.Menu {
	var items []*mygo.MenuItem
	trayMu.Lock()
	devs, auto := trayDevs, trayAuto
	trayMu.Unlock()
	// Enabled rows: a device opens its page, the status opens the scenes page.
	const shown = 4 // more devices go to a submenu
	row := func(d trayDevice) *mygo.MenuItem {
		return &mygo.MenuItem{Label: d.Label, Click: func(*mygo.MenuItem, *mygo.Window) { openTarget(d.ID) }}
	}
	for i, d := range devs {
		if i == shown {
			var more []*mygo.MenuItem
			for _, d := range devs[shown:] {
				more = append(more, row(d))
			}
			items = append(items, &mygo.MenuItem{Label: tr("tray.moreDevices", len(more)), Submenu: more})
			break
		}
		items = append(items, row(d))
	}
	if auto != "" {
		items = append(items, &mygo.MenuItem{Label: auto, Click: func(*mygo.MenuItem, *mygo.Window) { openTarget("scenes") }})
	}
	if len(items) > 0 {
		items = append(items, mygo.Separator())
	}
	return mygo.NewMenu(append(items,
		&mygo.MenuItem{Label: tr("tray.open"), Click: func(*mygo.MenuItem, *mygo.Window) { openWindow() }},
		mygo.Separator(),
		&mygo.MenuItem{Label: tr("tray.smart"), Type: mygo.MenuItemCheckbox, Checked: Scenes{}.Smart().Enabled,
			Click: func(it *mygo.MenuItem, _ *mygo.Window) {
				c := Scenes{}.Smart()
				c.Enabled = it.Checked
				if err := (Scenes{}).SetSmart(c); err != nil {
					log.Println("smart poll:", err)
				}
			}},
		&mygo.MenuItem{Label: tr("tray.scenes"), Type: mygo.MenuItemCheckbox, Checked: Scenes{}.Config().Enabled,
			Click: func(it *mygo.MenuItem, _ *mygo.Window) {
				c := Scenes{}.Config()
				c.Enabled = it.Checked
				if err := (Scenes{}).SetConfig(c); err != nil {
					log.Println("scenes:", err)
				}
			}},
		mygo.Separator(),
		&mygo.MenuItem{Label: tr("tray.quit"), Role: mygo.RoleQuit},
	))
}

func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running instance opens its window
	}
	loadConfig()
	if err := devices.rescan(); err != nil {
		log.Println("scan:", err)
	}
	if err := dongles.rescan(); err != nil {
		log.Println("scan audio:", err)
	}
	if err := keyboards.rescan(); err != nil {
		log.Println("scan keyboards:", err)
	}
	loadPendingAudio() // after the scan, which forgets dongles that are gone
	go runAuto()
	go watchDevices()
	go runHooks()
	go keepBindings()
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		restoreBindingsOnQuit() // buttons work without the app
		restoreAudioOnQuit()    // and sound plays as it did before scenes
	})

	// The update window in the app's language; it keeps it until a restart.
	mygo.Use(updater.New(updater.Options{Language: language(), Icon: appIcon}))
	mygo.Bind(Devices{})
	mygo.Bind(Audio{})
	mygo.Bind(Keyboards{})
	mygo.Bind(Macros{})
	mygo.Bind(Scenes{})
	mygo.Bind(Settings{})

	mygo.App.OnSecondInstance(func([]string, string) { openWindow() })
	mygo.App.OnWindowAllClosed(func() {}) // keep running in the tray
	mygo.App.WhenReady(func() {
		t, err := mygo.NewTray(mygo.TrayOptions{ID: "opensynapse", Icon: trayIcon, ToolTip: "OpenSynapse"})
		if err != nil {
			log.Println("tray:", err)
		} else {
			tray := t
			trayMu.Lock()
			trayOf = t
			trayMu.Unlock()
			tray.OnClick(openWindow)
			tray.OnRightClick(func() {
				tray.SetMenu(trayMenu()) // rebuilt each time so the checkbox is current
				tray.PopUpMenu()
				tray.SetMenu(nil)
			})
		}
		go runBattery() // after the tray exists, so the first reading reaches its tooltip
		watchUpdater()  // the plugin is set up once the app is ready
		watchDPI()
		if !slices.Contains(os.Args[1:], "--tray") || err != nil {
			openWindow()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Settings holds app-level options.
type Settings struct{}

// Autostart reports whether OpenSynapse starts (to the tray) at login.
func (Settings) Autostart() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("OpenSynapse")
	return err == nil
}

// SetAutostart turns starting at login on or off.
func (Settings) SetAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue("OpenSynapse"); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue("OpenSynapse", `"`+exe+`" --tray`)
}

// Notify reports whether low-battery notifications are on.
func (Settings) Notify() bool {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return cfg.Notify
}

// SetNotify turns low-battery notifications on or off.
func (Settings) SetNotify(on bool) error {
	cfgMu.Lock()
	cfg.Notify = on
	cfgMu.Unlock()
	return saveConfig()
}

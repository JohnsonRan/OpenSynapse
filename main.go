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

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows/registry"
)

//go:embed resources/tray.png
var trayIcon []byte

var window *mygo.Window

func openWindow() {
	if window != nil {
		window.Restore()
		window.Focus()
		return
	}
	window = mygo.NewWindow(mygo.WindowOptions{
		Title:           "OpenSynapse",
		Width:           1180,
		Height:          760,
		MinWidth:        960,
		MinHeight:       620,
		BackgroundColor: "#0a0a0a",
		// No title bar; Windows keeps its own min/max/close (and Snap) over the page.
		TitleBarStyle:  mygo.TitleBarHidden,
		TitleBarHeight: 40,
		StateKey:        "main",
		URL:             "/",
	})
	// Closing destroys the webview and its browser processes; the tray keeps running.
	window.OnClosed(func() { window = nil })
}

func trayMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "打开 OpenSynapse", Click: func(*mygo.MenuItem, *mygo.Window) { openWindow() }},
		{Label: "智能回报率", Type: mygo.MenuItemCheckbox, Checked: Smart{}.Config().Enabled,
			Click: func(it *mygo.MenuItem, _ *mygo.Window) {
				c := Smart{}.Config()
				c.Enabled = it.Checked
				if err := (Smart{}).SetConfig(c); err != nil {
					log.Println("smart:", err)
				}
			}},
		mygo.Separator(),
		{Label: "退出", Role: mygo.RoleQuit},
	})
}

func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running instance opens its window
	}
	loadConfig()
	if err := devices.rescan(); err != nil {
		log.Println("scan:", err)
	}
	go runSmart()

	mygo.Bind(Devices{})
	mygo.Bind(Smart{})
	mygo.Bind(Settings{})

	mygo.App.OnSecondInstance(func([]string, string) { openWindow() })
	mygo.App.OnWindowAllClosed(func() {}) // keep running in the tray
	mygo.App.WhenReady(func() {
		tray, err := mygo.NewTray(mygo.TrayOptions{ID: "opensynapse", Icon: trayIcon, ToolTip: "OpenSynapse"})
		if err != nil {
			log.Println("tray:", err)
		} else {
			tray.OnClick(openWindow)
			tray.OnRightClick(func() {
				tray.SetMenu(trayMenu()) // rebuilt each time so the checkbox is current
				tray.PopUpMenu()
				tray.SetMenu(nil)
			})
		}
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

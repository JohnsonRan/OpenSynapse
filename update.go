package main

import (
	"errors"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
)

// Updates come from the signed GitHub releases (mygo.config.ts, updates).
// MyGo's updater plugin checks in the background and shows its own window;
// it replaces the files of the app's folder, so it updates the installed
// version (%LOCALAPPDATA%\Programs) and a writable portable folder alike.

// AppInfo is what the About page shows.
type AppInfo struct {
	Version   string `json:"version"`
	Portable  bool   `json:"portable"`
	CanUpdate bool   `json:"canUpdate"` // false for development builds and read-only folders
	AutoCheck bool   `json:"autoCheck"` // background update checks
	LastCheck int64  `json:"lastCheck"` // Unix ms of the last successful check, 0 if none
}

// AboutChanged tells the window that a check ran or an update preference changed.
var AboutChanged = mygo.NewEvent[AppInfo]("aboutChanged")

// watchUpdater keeps an open About page current; the plugin calls on the main thread.
func watchUpdater() {
	updater.OnChange(func() {
		if w := currentWindow(); w != nil {
			_ = AboutChanged.Emit(w, Settings{}.App())
		}
	})
}

// App returns the running version and the update state.
func (Settings) App() AppInfo {
	info := AppInfo{
		Version:   mygo.App.Version(),
		Portable:  portableDir != "",
		CanUpdate: mygo.Updater.Enabled(),
		AutoCheck: updater.AutomaticChecks(),
	}
	if t := updater.LastCheck(); !t.IsZero() {
		info.LastCheck = t.UnixMilli()
	}
	return info
}

// SetAutoUpdate turns background update checks on or off.
func (Settings) SetAutoUpdate(on bool) { updater.SetAutomaticChecks(on) }

// CheckForUpdates checks now; the update window shows the result.
func (Settings) CheckForUpdates() { updater.CheckForUpdates() }

const repoURL = "https://github.com/JohnsonRan/OpenSynapse"

// links the page may open; it names one instead of passing a URL.
var links = map[string]string{
	"repo":      repoURL,
	"releases":  repoURL + "/releases",
	"changelog": repoURL + "/blob/master/CHANGELOG.md",
	"issues":    repoURL + "/issues",
	"license":   repoURL + "/blob/master/LICENSE",
	"openrazer": "https://github.com/openrazer/openrazer",
}

// OpenLink opens one of the project's pages in the browser.
func (Settings) OpenLink(name string) error {
	u, ok := links[name]
	if !ok {
		return errors.New("unknown link")
	}
	return mygo.Shell.OpenExternal(u)
}

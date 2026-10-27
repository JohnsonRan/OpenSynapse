package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
)

// Updates come from the signed GitHub releases (mygo.config.ts, updates).
// MyGo's updater plugin checks in the background and shows its own window;
// it replaces the files of the app's folder, so it updates the installed
// version (%LOCALAPPDATA%\Programs) and a writable portable folder alike.

// AppInfo is the running version and how it was installed.
type AppInfo struct {
	Version  string `json:"version"`
	Portable bool   `json:"portable"`
}

// App returns the running version.
func (Settings) App() AppInfo { return AppInfo{mygo.App.Version(), portableDir != ""} }

// AutoUpdate reports whether OpenSynapse checks for updates in the background.
func (Settings) AutoUpdate() bool { return updater.AutomaticChecks() }

// SetAutoUpdate turns background update checks on or off.
func (Settings) SetAutoUpdate(on bool) { updater.SetAutomaticChecks(on) }

// CheckForUpdates checks now; the update window shows the result.
func (Settings) CheckForUpdates() { updater.CheckForUpdates() }

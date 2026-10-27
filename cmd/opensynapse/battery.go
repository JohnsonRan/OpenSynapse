package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

var (
	trayMu   sync.Mutex
	trayOf   *mygo.Tray
	trayAuto string // active scene and polling rate
	trayDevs []trayDevice
)

// trayDevice is one row of the tray menu.
type trayDevice struct{ ID, Label string }

// trayLabel: "DeathAdder V4 Pro · 有线 · 95% ⚡"; the connection is named only
// for products that come in a wired and a wireless version.
func trayLabel(name string, wired, wireless bool, pct int, charging bool) string {
	parts := []string{strings.TrimPrefix(name, "Razer ")}
	if wired {
		parts = append(parts, tr("tray.wired"))
	} else if wireless {
		parts = append(parts, tr("tray.wireless"))
	}
	if pct >= 0 {
		b := fmt.Sprintf("%d%%", pct)
		if charging {
			b += " ⚡"
		}
		parts = append(parts, b)
	}
	return strings.Join(parts, " · ")
}

// rateLabel names a smart polling target.
func rateLabel(hz int) string {
	if hz == 0 {
		return tr("rate.highest")
	}
	return fmt.Sprintf("%d Hz", hz)
}

// setTrayStatus shows the active scene and polling rate in the tray tooltip.
func setTrayStatus(st AutoStatus) {
	var parts []string
	if st.Rate >= 0 {
		parts = append(parts, tr("tray.rate", rateLabel(st.Rate)))
	}
	if st.Scene != "" {
		parts = append(parts, tr("tray.scene", st.Scene))
	}
	text := strings.Join(parts, " · ")
	trayMu.Lock()
	defer trayMu.Unlock()
	if text == trayAuto {
		return
	}
	trayAuto = text
	updateTooltip()
}

// updateTooltip needs trayMu held.
func updateTooltip() {
	if trayOf == nil {
		return
	}
	tip := "OpenSynapse"
	if trayAuto != "" {
		tip += " · " + trayAuto
	}
	for _, d := range trayDevs {
		tip += "\n" + d.Label
	}
	if r := []rune(tip); len(r) > 120 { // the Windows tooltip holds 127 characters
		tip = string(r[:120]) + "…"
	}
	trayOf.SetToolTip(tip)
}

// lowBattery decides whether to warn: once per discharge, re-armed when the
// device charges or climbs 5 points above the threshold.
type lowBattery struct{ warned map[string]bool }

func (l *lowBattery) check(id string, pct, threshold int, charging bool) (warn bool) {
	if charging || pct > threshold+5 {
		delete(l.warned, id)
		return false
	}
	if pct <= threshold && !l.warned[id] {
		l.warned[id] = true
		return true
	}
	return false
}

var (
	refreshMu sync.Mutex
	lowBat    = lowBattery{warned: map[string]bool{}}
)

// runBattery refreshes the tray every 2 minutes.
func runBattery() {
	for ; ; time.Sleep(2 * time.Minute) {
		refreshTray()
	}
}

// refreshTray reads battery levels for the tray menu and tooltip, and sends
// low-battery notifications.
func refreshTray() {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	var rows []trayDevice
	for _, d := range devices.list() {
		wireless := strings.HasSuffix(d.Name, " (Wireless)")
		pct, charging := -1, false
		if d.Features().Battery {
			if p, err := d.Battery(); err == nil {
				pct = p
				charging, _ = d.Charging()
			}
		}
		rows = append(rows, trayDevice{d.ID, trayLabel(d.Family(), d.Wired(), wireless, pct, charging)})
		if pct < 0 {
			continue // asleep or no battery: no news is not low battery
		}
		threshold := 20
		if d.Features().LowBattery {
			if t, err := d.LowBattery(); err == nil {
				threshold = t
			}
		}
		cfgMu.Lock()
		notify := cfg.Notify
		cfgMu.Unlock()
		if lowBat.check(d.ID, pct, threshold, charging) && notify {
			n := mygo.NewNotification(mygo.NotificationOptions{
				ID:    "battery-" + d.ID,
				Title: tr("battery.title", d.Name),
				Body:  tr("battery.body", pct),
			})
			if err := n.Show(); err != nil {
				log.Println("notify:", err)
			}
		}
	}
	trayMu.Lock()
	trayDevs = rows
	updateTooltip()
	trayMu.Unlock()
}

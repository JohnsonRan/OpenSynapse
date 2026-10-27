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
	trayMu      sync.Mutex
	trayOf      *mygo.Tray
	trayAuto    string // active scene and polling rate
	trayBattery string
)

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
	if st.Scene != "" {
		parts = append(parts, st.Scene)
	}
	if st.Rate >= 0 {
		parts = append(parts, rateLabel(st.Rate))
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
	if trayBattery != "" {
		tip += "\n" + trayBattery
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

// runBattery reads battery levels every 2 minutes for the tray tooltip and
// low-battery notifications.
func runBattery() {
	lb := lowBattery{warned: map[string]bool{}}
	for ; ; time.Sleep(2 * time.Minute) {
		var lines []string
		for _, d := range devices.list() {
			if !d.Features().Battery {
				continue
			}
			pct, err := d.Battery()
			if err != nil {
				continue // asleep: no news is not low battery
			}
			charging, _ := d.Charging()
			threshold := 20
			if d.Features().LowBattery {
				if t, err := d.LowBattery(); err == nil {
					threshold = t
				}
			}
			mark := ""
			if charging {
				mark = " ⚡"
			}
			lines = append(lines, fmt.Sprintf("%s %d%%%s", strings.TrimPrefix(d.Name, "Razer "), pct, mark))
			cfgMu.Lock()
			notify := cfg.Notify
			cfgMu.Unlock()
			if lb.check(d.ID, pct, threshold, charging) && notify {
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
		trayBattery = strings.Join(lines, "\n")
		updateTooltip()
		trayMu.Unlock()
	}
}

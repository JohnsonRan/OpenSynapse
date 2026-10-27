package main

import (
	"errors"
	"net/url"

	"github.com/egoist/mygo"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

// DeviceDetails is what the device page's About section shows.
type DeviceDetails struct {
	Firmware string `json:"firmware"` // "" when the device does not say
	Serial   string `json:"serial"`
	Pairing  bool   `json:"pairing"` // has a receiver to pair with
}

// Details reads the firmware version and serial number. Through a receiver
// they are the mouse's.
func (Devices) Details(id string) (DeviceDetails, error) {
	d, err := devices.get(id)
	if err != nil {
		return DeviceDetails{}, err
	}
	fw, _ := d.Firmware()
	sn, _ := d.Serial()
	return DeviceDetails{Firmware: fw, Serial: sn, Pairing: d.Features().ReceiverLEDs}, nil
}

// Razer support answers by product family. Firmware updates and receiver
// pairing need Razer's own tools; the page links to them.
// shortcut: only the DeathAdder V4 Pro's firmware page is known; others get a support search.
var firmwarePages = map[string]string{
	"Razer DeathAdder V4 Pro": "https://mysupport.razer.com/app/answers/detail/a_id/20103",
}

// Pairing: Razer's guide (a_id 6143) goes through Synapse, and no standalone
// utility for every mouse is known, so the link searches Razer support for one.

// OpenSupport opens Razer's firmware updater ("firmware") or receiver
// pairing ("pairing") page for the device in the browser.
func (Devices) OpenSupport(id, kind string) error {
	d, err := devices.get(id)
	if err != nil {
		return err
	}
	switch kind {
	case "firmware":
		u, ok := firmwarePages[d.Family()]
		if !ok {
			u = "https://mysupport.razer.com/app/answers/list/kw/" + url.PathEscape(d.Family()+" firmware updater")
		}
		return mygo.Shell.OpenExternal(u)
	case "pairing":
		return mygo.Shell.OpenExternal("https://mysupport.razer.com/app/answers/list/kw/" + url.PathEscape(d.Family()+" pairing utility"))
	}
	return errors.New("unknown support page")
}

// Tracking reads the lift-off distance; it fails on devices without it.
func (Devices) Tracking(id string) (razer.Tracking, error) {
	d, err := devices.get(id)
	if err != nil {
		return razer.Tracking{}, err
	}
	return d.Tracking()
}

// SetTracking sets the lift-off distance.
func (Devices) SetTracking(id string, t razer.Tracking) error {
	d, err := devices.get(id)
	if err != nil {
		return err
	}
	return d.SetTracking(t)
}

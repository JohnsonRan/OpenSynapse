package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"syscall"
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

func dev(id, name string, answered bool) *razer.Device {
	return &razer.Device{Model: &razer.Model{Name: name}, ID: id, Answered: answered}
}

func ids(devs []*razer.Device) (out []string) {
	for _, d := range devs {
		out = append(out, d.ID)
	}
	return out
}

func TestConnectedPairsWiredAndWireless(t *testing.T) {
	kb := dev("kb", "Razer BlackWidow V3", true)
	cases := []struct {
		name string
		devs []*razer.Device
		want string
	}{
		// cable plugged: the receiver stays attached but its mouse answers on the cable
		{"cable", []*razer.Device{dev("BF", "Razer DeathAdder V4 Pro (Wireless)", false), dev("BE", "Razer DeathAdder V4 Pro (Wired)", true), kb}, "BE"},
		// both answer (receiver relays too): prefer the cable
		{"both", []*razer.Device{dev("BF", "Razer DeathAdder V4 Pro (Wireless)", true), dev("BE", "Razer DeathAdder V4 Pro (Wired)", true), kb}, "BE"},
		// wired ID enumerated but silent (charging only): wireless wins
		{"charging", []*razer.Device{dev("BF", "Razer DeathAdder V4 Pro (Wireless)", true), dev("BE", "Razer DeathAdder V4 Pro (Wired)", false), kb}, "BF"},
		// nothing answers (mouse asleep): keep the wireless ID
		{"asleep", []*razer.Device{dev("BE", "Razer DeathAdder V4 Pro (Wired)", false), dev("BF", "Razer DeathAdder V4 Pro (Wireless)", false), kb}, "BF"},
	}
	for _, c := range cases {
		got := ids(connected(c.devs))
		if len(got) != 2 || got[1] != "kb" || got[0] != c.want {
			t.Errorf("%s: got %v, want [%s kb]", c.name, got, c.want)
		}
	}
	if got := ids(connected([]*razer.Device{dev("BF", "Razer DeathAdder V4 Pro (Wireless)", false)})); len(got) != 1 {
		t.Errorf("a lone silent device must stay listed: %v", got)
	}
}

// The page hides the rotation card for "this device has none" (nil) and asks
// again later only after "no answer" (an error). Neither device here has a
// HID collection, so nothing is sent.
func TestRotationBinding(t *testing.T) {
	other := &razer.Device{Model: &razer.Model{Name: "Razer Viper V3 Pro (Wired)", Category: "mouse"}, ID: "other"}
	v4 := &razer.Device{Model: &razer.Model{Name: "Razer DeathAdder V4 Pro (Wireless)", Category: "mouse"}, ID: "v4"}
	devices.mu.Lock()
	saved := devices.devs
	devices.devs = []*razer.Device{other, v4}
	devices.mu.Unlock()
	t.Cleanup(func() {
		devices.mu.Lock()
		devices.devs = saved
		devices.mu.Unlock()
	})

	if r, err := (Devices{}).Rotation("other"); r != nil || err != nil {
		t.Errorf("a mouse without rotation: %+v %v, want nil and no error", r, err)
	}
	// Without a HID collection the read fails, but not with "no answer".
	if r, err := (Devices{}).Rotation("v4"); r != nil || err != nil {
		t.Errorf("a DeathAdder V4 Pro whose read fails: %+v %v, want nil and no error", r, err)
	}
	on := razer.Rotation{Enabled: true, Angle: -20}
	if r, err := rotationRead(on, nil); err != nil || r == nil || *r != on {
		t.Errorf("a read that worked: %+v %v, want %+v", r, err, on)
	}
	for _, asleep := range []error{razer.ErrNoAnswer, fmt.Errorf("receiver: %w", razer.ErrNoAnswer)} {
		if r, err := rotationRead(razer.Rotation{}, asleep); r != nil || !errors.Is(err, razer.ErrNoAnswer) {
			t.Errorf("%v: %+v %v, want the error, so that the page asks again", asleep, r, err)
		}
	}
	for _, without := range []error{razer.ErrUnsupported, razer.ErrClosed, errors.New("device status 0x03")} {
		if r, err := rotationRead(on, without); r != nil || err != nil {
			t.Errorf("%v: %+v %v, want nil and no error", without, r, err)
		}
	}
	set := razer.Rotation{Enabled: true, Angle: 20}
	for _, id := range []string{"other", "v4"} {
		if err := (Devices{}).SetRotation(id, set); err == nil || err.Error() != tr("err.rotationSet") {
			t.Errorf("%s: write %v, want %q", id, err, tr("err.rotationSet"))
		}
	}
	if err := rotationError("v4", razer.ErrNotKept); err == nil || err.Error() != tr("err.rotationKept") {
		t.Errorf("a value the mouse did not keep: %v", err)
	}
	if err := rotationError("v4", razer.ErrNoAnswer); err == nil || err.Error() != tr("err.rotationSet") {
		t.Errorf("no answer: %v", err)
	}
	if err := rotationError("v4", nil); err != nil {
		t.Errorf("success: %v", err)
	}
}

// A failed rotation write is logged only when it is not an expected case.
func TestRotationErrorLog(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	for name, c := range map[string]struct {
		err    error
		logged bool
	}{
		"asleep":             {razer.ErrNoAnswer, false},
		"no rotation":        {razer.ErrUnsupported, false},
		"replaced by scan":   {razer.ErrClosed, false},
		"unplugged":          {fmt.Errorf("HidD_SetFeature: %w", syscall.Errno(1167)), false}, // ERROR_DEVICE_NOT_CONNECTED
		"failure status":     {errors.New("device status 0x03"), true},
		"value not kept":     {razer.ErrNotKept, true},
		"nothing went wrong": {nil, false},
	} {
		buf.Reset()
		rotationError("v4", c.err)
		if logged := buf.Len() > 0; logged != c.logged {
			t.Errorf("%s: logged %v (%q), want %v", name, logged, buf.String(), c.logged)
		}
	}
}

func TestTrayLabel(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{trayLabel("Razer DeathAdder V4 Pro", true, false, 95, true), "DeathAdder V4 Pro · " + tr("tray.wired") + " · 95% ⚡"},
		{trayLabel("Razer DeathAdder V4 Pro", false, true, 40, false), "DeathAdder V4 Pro · " + tr("tray.wireless") + " · 40%"},
		{trayLabel("Razer BlackWidow V3", false, false, -1, false), "BlackWidow V3"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

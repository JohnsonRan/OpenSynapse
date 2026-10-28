package razer

import (
	"errors"
	"fmt"
	"strings"
)

// Firmware returns the device's firmware version, e.g. "1.21". Through a
// receiver it is the mouse's, not the receiver's.
func (d *Device) Firmware() (string, error) {
	r, err := d.query("standard_get_firmware_version", 0x00, 0x81, 0x02)
	if err != nil {
		return "", err
	}
	a := r.Args()
	return fmt.Sprintf("%d.%02d", a[0], a[1]), nil
}

// Serial returns the device's serial number.
func (d *Device) Serial() (string, error) {
	r, err := d.query("standard_get_serial", 0x00, 0x82, 0x16)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.TrimRight(string(r.Args()[:0x16]), "\x00")), nil
}

// Lift-off distance. These commands come from Synapse 4 (class 0x0B, the
// proximity sensor) and have only been tried on the DeathAdder V4 Pro.

// Tracking is the lift-off setting: one distance for lifting and landing, or
// separate ones (asymmetric).
type Tracking struct {
	Asymmetric bool `json:"asymmetric"`
	Distance   int  `json:"distance"` // 1 low, 2 medium, 3 high
	LiftOff    int  `json:"liftOff"`  // asymmetric: 2–26
	Landing    int  `json:"landing"`  // asymmetric: 1–25, below LiftOff
}

// DefaultTracking is what the DeathAdder V4 Pro ships with (Synapse's defaults).
var DefaultTracking = Tracking{Distance: 2, LiftOff: 13, Landing: 12}

const (
	sensorClass      = 0x00
	liftCutOff       = 1 // Universal_Lift_Cut_off
	liftPresetHyster = 4 // Preset_Calibration_Hysteresis
)

// sensorID returns the proximity sensor's id; ErrUnsupported without one.
func (d *Device) sensorID() (byte, error) {
	r, err := d.run("synapse_sensor", 0x0B, 0x80, 2)
	if err != nil {
		return 0, err
	}
	if a := r.Args(); a[0] > 0 && a[1] != 0 {
		return a[1], nil
	}
	return 0, ErrUnsupported
}

// Tracking reads the lift-off setting.
func (d *Device) Tracking() (Tracking, error) {
	id, err := d.sensorID()
	if err != nil {
		return Tracking{}, err
	}
	get := func(cmd, size byte) ([]byte, error) {
		r, err := d.run("synapse_sensor", 0x0B, cmd, size, sensorClass, id)
		if err != nil {
			return nil, err
		}
		return r.Args()[2:], nil
	}
	state, err := get(0x83, 3)
	if err != nil {
		return Tracking{}, err
	}
	lift, err := get(0x8B, 4)
	if err != nil {
		return Tracking{}, err
	}
	t := DefaultTracking
	if state[0] == 0 {
		t.Distance = int(lift[1]) + 1
		return t, t.check()
	}
	cfg, err := get(0x85, 10)
	if err != nil {
		return Tracking{}, err
	}
	// The reply has one byte more before the distances than the write takes.
	t.Asymmetric, t.LiftOff, t.Landing = true, int(cfg[1])+1, int(cfg[2])+1
	return t, t.check()
}

func (t Tracking) check() error {
	if !t.Asymmetric && (t.Distance < 1 || t.Distance > 3) {
		return errors.New("lift-off distance must be 1–3")
	}
	if t.Asymmetric && (t.LiftOff < 2 || t.LiftOff > 26 || t.Landing < 1 || t.Landing >= t.LiftOff) {
		return errors.New("lift-off must be 2–26 and landing below it")
	}
	return nil
}

// SetTracking writes the lift-off setting, as Synapse does.
func (d *Device) SetTracking(t Tracking) error {
	if err := t.check(); err != nil {
		return err
	}
	id, err := d.sensorID()
	if err != nil {
		return err
	}
	set := func(cmd, size byte, args ...byte) error {
		_, err := d.run("synapse_sensor", 0x0B, cmd, size, append([]byte{sensorClass, id}, args...)...)
		return err
	}
	if !t.Asymmetric {
		if err := set(0x03, 3, 0); err != nil {
			return err
		}
		return set(0x0B, 4, liftCutOff, byte(t.Distance-1))
	}
	if err := set(0x03, 3, 1); err != nil {
		return err
	}
	if err := set(0x0B, 4, liftPresetHyster, 0); err != nil {
		return err
	}
	return set(0x05, 10, byte(t.LiftOff-1), byte(t.Landing-1))
}

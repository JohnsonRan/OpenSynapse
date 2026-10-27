package razer

import (
	"errors"
	"slices"
)

// Button remapping. These commands come from Synapse 4, not openrazer, and
// have only been tried on the DeathAdder V4 Pro; a device without them does
// not answer. Class 0x02: 0x84 lists the buttons, 0x8C/0x0C get/set one
// button as [profile, button, layer, function, data size, data...]; the
// reply's layer byte reads 1 whatever was asked.

// Assignment is what a button does: a Synapse function id and its data.
type Assignment struct {
	Fn   byte   `json:"fn"`
	Data []byte `json:"data"`
}

// Function ids (Synapse's BUTTONKEY_FUNCTION_ID).
const (
	FnMouse = 1 // data: mouse button code (1 left … 5 forward, 9/10 wheel)
	FnKey   = 2 // data: modifier bits, HID keyboard usage
	FnDPI   = 6 // data: 1 up, 2 down, 6 cycle up, 7 cycle down
	// FnHypershift: while the button is held, the other buttons do what their
	// Hypershift layer says (data [1], as Synapse writes it).
	FnHypershift = 12
)

// Button layers: what a button does normally, and while a Hypershift button is held.
const (
	LayerNormal     = 0
	LayerHypershift = 1
)

// Buttons lists the device's remappable button ids.
func (d *Device) Buttons() ([]int, error) {
	r, err := d.run("synapse_buttons", 0x02, 0x84, 80)
	if err != nil {
		return nil, err
	}
	a := r.Args()
	n := int(a[0])
	if n == 0 || n >= len(a) {
		return nil, ErrUnsupported
	}
	out := make([]int, n)
	for i := range out {
		out[i] = int(a[1+i])
	}
	return out, nil
}

// ButtonAssignment reads what a button does now on a layer.
func (d *Device) ButtonAssignment(button, layer int) (Assignment, error) {
	r, err := d.run("synapse_buttons", 0x02, 0x8C, 80, noStore, byte(button), byte(layer))
	if err != nil {
		return Assignment{}, err
	}
	a := r.Args()
	n := int(a[4])
	if a[1] != byte(button) || 5+n > len(a) {
		return Assignment{}, errors.New("unexpected button assignment reply")
	}
	return Assignment{Fn: a[3], Data: slices.Clone(a[5 : 5+n])}, nil
}

// SetButtonAssignment changes what a button does now. On the DeathAdder V4
// Pro only profile 0 (noStore) takes effect; profile 1 (varStore) accepts the
// write but the button keeps its old function.
func (d *Device) SetButtonAssignment(button, layer int, as Assignment) error {
	if len(as.Data) > 75 {
		return errors.New("button data too long")
	}
	args := append([]byte{noStore, byte(button), byte(layer), as.Fn, byte(len(as.Data))}, as.Data...)
	_, err := d.run("synapse_buttons", 0x02, 0x0C, 80, args...)
	return err
}

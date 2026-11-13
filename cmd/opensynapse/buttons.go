package main

import (
	"errors"
	"slices"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

// Buttons can play a macro (macro.go), or be remapped on the device itself:
// to a mouse button, a DPI action, a sensitivity clutch, a key, or Hypershift.
// A remap is stored in the mouse and works without OpenSynapse.
//
// Each button has two layers: what it does normally, and what it does while
// a button set to Hypershift is held.

// ButtonAction is what a button does, as the page shows and sets it.
type ButtonAction struct {
	Kind  string `json:"kind"`          // default, macro, mouse, dpi, clutch, key, hypershift, other
	Macro string `json:"macro"`         // macro: its ID
	Code  int    `json:"code"`          // mouse: 1–5, 9, 10; dpi: 1 up, 2 down, 6 cycle up, 7 cycle down; key: virtual-key code, 0 for modifiers only
	Mods  int    `json:"mods"`          // key: modifier bits, left Ctrl 1, Shift 2, Alt 4, Win 8
	DPI   int    `json:"dpi,omitempty"` // clutch: the DPI while the button is held
}

// ButtonInfo is a remappable button and what it does on each layer.
type ButtonInfo struct {
	ID     int          `json:"id"`
	Action ButtonAction `json:"action"` // normally
	Shift  ButtonAction `json:"shift"`  // while Hypershift is held
}

var (
	mouseCodes = []int{1, 2, 3, 4, 5, 9, 10}
	dpiCodes   = []int{1, 2, 6, 7}
)

// mouseModel is a catalog model of the family, nil for none. A binding names
// its mouse by family alone, and the wired and wireless models of one family
// take the same button functions.
func mouseModel(family string) *razer.Model {
	for i := range razer.Models {
		if m := &razer.Models[i]; m.Family() == family {
			return m
		}
	}
	return nil
}

// actionOf describes a device assignment of a mouse of model m (nil when the
// model is not known). The Hypershift layer's default is the button's normal
// function, as the DeathAdder V4 Pro ships.
func actionOf(m *razer.Model, button int, a razer.Assignment) ButtonAction {
	def := defaultAssignment(button)
	switch {
	case a.Fn == def.Fn && slices.Equal(a.Data, def.Data):
		return ButtonAction{Kind: "default"}
	case a.Fn == razer.FnMouse && len(a.Data) == 1:
		return ButtonAction{Kind: "mouse", Code: int(a.Data[0])}
	case a.Fn == razer.FnDPI && len(a.Data) >= 1 && a.Data[0] == razer.DPIClutch:
		// A clutch the app could not write again (another model, X and Y
		// apart, a DPI out of range) is "other", not a DPI action.
		if m != nil {
			if dpi, ok := m.ClutchDPI(a); ok {
				return ButtonAction{Kind: "clutch", DPI: dpi}
			}
		}
	case a.Fn == razer.FnDPI && len(a.Data) >= 1:
		return ButtonAction{Kind: "dpi", Code: int(a.Data[0])}
	case a.Fn == razer.FnHypershift:
		return ButtonAction{Kind: "hypershift"}
	case a.Fn == razer.FnKey && len(a.Data) == 2:
		if vk, ok := hidToVK[a.Data[1]]; ok || a.Data[1] == 0 {
			return ButtonAction{Kind: "key", Code: vk, Mods: int(a.Data[0])}
		}
	}
	return ButtonAction{Kind: "other"}
}

// assignmentOf is the device assignment of an action other than a macro, for
// a mouse of model m (nil when the model is not known).
func assignmentOf(m *razer.Model, button, layer int, act ButtonAction) (razer.Assignment, error) {
	switch act.Kind {
	case "default":
		return defaultAssignment(button), nil
	case "clutch": // refused for a model that is not offered it, and for a DPI out of range
		if m != nil {
			if a, err := m.ClutchAssignment(act.DPI); err == nil {
				return a, nil
			}
		}
	case "mouse":
		if slices.Contains(mouseCodes, act.Code) {
			return razer.Assignment{Fn: razer.FnMouse, Data: []byte{byte(act.Code)}}, nil
		}
	case "dpi":
		if slices.Contains(dpiCodes, act.Code) {
			return razer.Assignment{Fn: razer.FnDPI, Data: []byte{byte(act.Code)}}, nil
		}
	case "hypershift": // a Hypershift button on its own layer would be stuck
		if layer == razer.LayerNormal {
			return razer.Assignment{Fn: razer.FnHypershift, Data: []byte{1}}, nil
		}
	case "key":
		usage, ok := vkToHID[act.Code]
		if bit, mod := modifierBits[act.Code]; mod {
			usage, ok, act.Mods = 0, true, act.Mods|int(bit)
		}
		if ok && act.Mods >= 0 && act.Mods < 256 && (usage != 0 || act.Mods != 0) {
			return razer.Assignment{Fn: razer.FnKey, Data: []byte{byte(act.Mods), usage}}, nil
		}
	}
	return razer.Assignment{}, errors.New(tr("err.buttonAction"))
}

// buttonAction reads what a button does on a layer, a macro binding first.
func buttonAction(d *razer.Device, button, layer int) (ButtonAction, error) {
	cfgMu.Lock()
	for _, bd := range cfg.Bindings {
		if bd.Family == d.Family() && bd.Button == button && bd.Layer == layer {
			cfgMu.Unlock()
			return ButtonAction{Kind: "macro", Macro: bd.Macro}, nil
		}
	}
	cfgMu.Unlock()
	a, err := d.ButtonAssignment(button, layer)
	if err != nil {
		return ButtonAction{}, err
	}
	return actionOf(d.Model, button, a), nil
}

// Buttons lists the device's remappable buttons. It fails on devices without
// Synapse's button commands.
func (Devices) Buttons(id string) ([]ButtonInfo, error) {
	d, err := devices.get(id)
	if err != nil {
		return nil, err
	}
	ids, err := d.Buttons()
	if err != nil {
		return nil, err
	}
	out := []ButtonInfo{}
	for _, b := range ids {
		info := ButtonInfo{ID: b}
		if info.Action, err = buttonAction(d, b, razer.LayerNormal); err != nil {
			return nil, err
		}
		if info.Shift, err = buttonAction(d, b, razer.LayerHypershift); err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

// SetButton sets what a button does on a layer. The left button keeps
// clicking normally: as a key or a macro, the mouse could not click.
func (s Devices) SetButton(id string, button, layer int, act ButtonAction) error {
	if layer != razer.LayerNormal && layer != razer.LayerHypershift {
		return errors.New(tr("err.buttonAction"))
	}
	if button == leftButton && layer == razer.LayerNormal {
		return errors.New(tr("err.leftButton"))
	}
	if act.Kind == "macro" {
		return s.bindButton(id, button, layer, act.Macro)
	}
	d, err := devices.get(id)
	if err != nil {
		return err
	}
	a, err := assignmentOf(d.Model, button, layer, act)
	if err != nil {
		return err
	}
	bindMu.Lock()
	defer bindMu.Unlock()
	// One assignment is one report: a write that fails leaves the button as it
	// was, so there is no earlier value to read and put back.
	if err := d.SetButtonAssignment(button, layer, a); err != nil {
		return err
	}
	// A macro binding the button had ends; its trigger key is gone.
	cfgMu.Lock()
	n := len(cfg.Bindings)
	cfg.Bindings = slices.DeleteFunc(cfg.Bindings, func(b Binding) bool {
		return b.Family == d.Family() && b.Button == button && b.Layer == layer
	})
	changed := len(cfg.Bindings) != n
	cfgMu.Unlock()
	if !changed {
		return nil
	}
	refreshHooks()
	return saveConfig()
}

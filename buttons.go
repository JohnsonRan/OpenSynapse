package main

import (
	"errors"
	"slices"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

// Buttons can play a macro (macro.go), or be remapped on the device itself:
// to a mouse button, a DPI action or a key. A remap is stored in the mouse
// and works without OpenSynapse.

// ButtonAction is what a button does, as the page shows and sets it.
type ButtonAction struct {
	Kind  string `json:"kind"`  // default, macro, mouse, dpi, key, other
	Macro string `json:"macro"` // macro: its ID
	Code  int    `json:"code"`  // mouse: 1–5, 9, 10; dpi: 1 up, 2 down, 6 cycle up, 7 cycle down; key: virtual-key code, 0 for modifiers only
	Mods  int    `json:"mods"`  // key: modifier bits, left Ctrl 1, Shift 2, Alt 4, Win 8
}

// ButtonInfo is a remappable button and what it does.
type ButtonInfo struct {
	ID     int          `json:"id"`
	Action ButtonAction `json:"action"`
}

const fnDPI = 6 // Synapse's DPI function: data is the action

var (
	mouseCodes = []int{1, 2, 3, 4, 5, 9, 10}
	dpiCodes   = []int{1, 2, 6, 7}
)

// actionOf describes a device assignment.
func actionOf(button int, a razer.Assignment) ButtonAction {
	def := defaultAssignment(button)
	switch {
	case a.Fn == def.Fn && slices.Equal(a.Data, def.Data):
		return ButtonAction{Kind: "default"}
	case a.Fn == razer.FnMouse && len(a.Data) == 1:
		return ButtonAction{Kind: "mouse", Code: int(a.Data[0])}
	case a.Fn == fnDPI && len(a.Data) >= 1:
		return ButtonAction{Kind: "dpi", Code: int(a.Data[0])}
	case a.Fn == razer.FnKey && len(a.Data) == 2:
		if vk, ok := hidToVK[a.Data[1]]; ok || a.Data[1] == 0 {
			return ButtonAction{Kind: "key", Code: vk, Mods: int(a.Data[0])}
		}
	}
	return ButtonAction{Kind: "other"}
}

// assignmentOf is the device assignment of an action other than a macro.
func assignmentOf(button int, act ButtonAction) (razer.Assignment, error) {
	switch act.Kind {
	case "default":
		return defaultAssignment(button), nil
	case "mouse":
		if slices.Contains(mouseCodes, act.Code) {
			return razer.Assignment{Fn: razer.FnMouse, Data: []byte{byte(act.Code)}}, nil
		}
	case "dpi":
		if slices.Contains(dpiCodes, act.Code) {
			return razer.Assignment{Fn: fnDPI, Data: []byte{byte(act.Code)}}, nil
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
		cfgMu.Lock()
		for _, bd := range cfg.Bindings {
			if bd.Family == d.Family() && bd.Button == b {
				info.Action = ButtonAction{Kind: "macro", Macro: bd.Macro}
			}
		}
		cfgMu.Unlock()
		if info.Action.Kind == "" {
			a, err := d.ButtonAssignment(b)
			if err != nil {
				return nil, err
			}
			info.Action = actionOf(b, a)
		}
		out = append(out, info)
	}
	return out, nil
}

// SetButton sets what a button does. The left button keeps clicking: if it
// were a key or a macro, the mouse could not click.
func (s Devices) SetButton(id string, button int, act ButtonAction) error {
	if button == leftButton {
		return errors.New(tr("err.leftButton"))
	}
	if act.Kind == "macro" {
		return s.BindButton(id, button, act.Macro)
	}
	a, err := assignmentOf(button, act)
	if err != nil {
		return err
	}
	d, err := devices.get(id)
	if err != nil {
		return err
	}
	bindMu.Lock()
	defer bindMu.Unlock()
	if err := d.SetButtonAssignment(button, a); err != nil {
		return err
	}
	// A macro binding the button had ends; its trigger key is gone.
	cfgMu.Lock()
	n := len(cfg.Bindings)
	cfg.Bindings = slices.DeleteFunc(cfg.Bindings, func(b Binding) bool { return b.Family == d.Family() && b.Button == button })
	changed := len(cfg.Bindings) != n
	cfgMu.Unlock()
	if !changed {
		return nil
	}
	refreshHooks()
	return saveConfig()
}

package main

import (
	"errors"
	"log"
	"slices"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
)

// Keys of LEOBOG keyboards: a key can send another key (with modifiers), a
// media key, or play a macro. The keyboard cannot report its remaps, so the
// app keeps them; a macro key sends its F13–F24 trigger only while the app
// runs, and gets its own action back when the app quits.

// mediaCodes are the consumer usages offered: volume, playback and screen brightness.
var mediaCodes = []int{0xe9, 0xea, 0xe2, 0xcd, 0xb5, 0xb6, 0xb7, 0x6f, 0x70}

// KeyboardKeys is the keyboard's drawing and what each key does.
type KeyboardKeys struct {
	Keys    []leobog.Key   `json:"keys"`
	Width   int            `json:"width"`
	Height  int            `json:"height"`
	FnIndex int            `json:"fnIndex"` // the Fn key, which cannot be remapped
	Remap   bool           `json:"remap"`   // keys can be remapped and reset: the model's factory keys are known
	Normal  []ButtonAction `json:"normal"`  // by Keys position
	Fn      []ButtonAction `json:"fn"`      // while Fn is held
	Media   []int          `json:"media"`   // consumer usages a key can send
}

// keymapOf is what the keyboard should hold: the remembered remaps, with the
// macro keys sending their trigger unless restore.
func keymapOf(d *leobog.Device, restore bool) [leobog.Layers]leobog.Layer {
	m := keyboardMemory(d.ID)
	km := d.DefaultLayers()
	if m.Keymap != nil {
		km = *m.Keymap
	}
	if restore {
		return km
	}
	cfgMu.Lock()
	defer cfgMu.Unlock()
	for _, b := range cfg.Bindings {
		if b.Family == d.Name && b.Layer >= 0 && b.Layer < leobog.Layers && b.Button >= 0 && b.Button < leobog.Keys {
			km[b.Layer][b.Button] = leobog.Action{Type: leobog.ActionKey, Code: int(vkToHID[b.Trigger])}
		}
	}
	return km
}

// unknownKeymap marks a keyboard whose keymap was left unknown by a failed write.
var unknownKeymap [leobog.Layers]leobog.Layer

var (
	deviceReset     = (*leobog.Device).Reset
	deviceSetKeymap = (*leobog.Device).SetKeymap
)

// syncKeymaps writes the keymap to keyboards that hold another; bindMu is held.
func syncKeymaps(restore bool) {
	for _, d := range keyboards.list() {
		if !d.CanRemap() {
			continue
		}
		want := keymapOf(d, restore)
		keyboards.mu.Lock()
		have := keyboards.written[d.ID]
		keyboards.mu.Unlock()
		if have == nil { // nothing written since the scan: it holds what the app left
			l := keymapOf(d, true)
			have = &l
		}
		if *have == want {
			continue
		}
		if err := deviceSetKeymap(d, want); err != nil {
			log.Println("keymap of", d.ID, err)
			continue
		}
		keyboards.mu.Lock()
		keyboards.written[d.ID] = &want
		keyboards.mu.Unlock()
	}
}

func layoutIndex(d *leobog.Device, idx int) bool {
	return idx != d.FnIndex && slices.ContainsFunc(d.Layout, func(k leobog.Key) bool { return k.Index == idx })
}

// keyAction describes a key's action for the page.
func keyAction(a, def leobog.Action) ButtonAction {
	switch {
	case a == def:
		return ButtonAction{Kind: "default"}
	case a.Type == leobog.ActionKey:
		if vk, ok := hidToVK[byte(a.Code)]; (ok || a.Code == 0) && a.Code < 0x100 && a.Mods < 0x10 {
			return ButtonAction{Kind: "key", Code: vk, Mods: a.Mods}
		}
	case a.Type == leobog.ActionMedia && slices.Contains(mediaCodes, a.Code):
		return ButtonAction{Kind: "media", Code: a.Code}
	}
	return ButtonAction{Kind: "other"}
}

// actionFor is the keyboard action of a page action other than a macro.
func actionFor(act ButtonAction, def leobog.Action) (leobog.Action, error) {
	switch act.Kind {
	case "default":
		return def, nil
	case "key":
		u, ok := vkToHID[act.Code]
		if (!ok && act.Code != 0) || act.Mods < 0 || act.Mods > 15 || act.Code == 0 && act.Mods == 0 || isTriggerVK(act.Code) {
			break
		}
		return leobog.Action{Type: leobog.ActionKey, Mods: act.Mods, Code: int(u)}, nil
	case "media":
		if slices.Contains(mediaCodes, act.Code) {
			return leobog.Action{Type: leobog.ActionMedia, Code: act.Code}, nil
		}
	}
	return leobog.Action{}, errors.New(tr("err.buttonAction"))
}

// Keys returns the keyboard's keys and what they do.
func (Keyboards) Keys(id string) (KeyboardKeys, error) {
	d, err := keyboards.get(id)
	if err != nil {
		return KeyboardKeys{}, err
	}
	km, def := keymapOf(d, true), d.DefaultLayers()
	out := KeyboardKeys{Keys: d.Layout, Width: d.Width, Height: d.Height, FnIndex: d.FnIndex, Remap: d.CanRemap(), Media: mediaCodes}
	cfgMu.Lock()
	bs := slices.Clone(cfg.Bindings)
	cfgMu.Unlock()
	for _, k := range d.Layout {
		for l, dst := range []*[]ButtonAction{&out.Normal, &out.Fn} {
			act := keyAction(km[l][k.Index], def[l][k.Index])
			if i := slices.IndexFunc(bs, func(b Binding) bool { return b.Family == d.Name && b.Button == k.Index && b.Layer == l }); i >= 0 {
				act = ButtonAction{Kind: "macro", Macro: bs[i].Macro}
			}
			*dst = append(*dst, act)
		}
	}
	return out, nil
}

// SetKey sets what a key does on a layer (0 normal, 1 with Fn).
func (Keyboards) SetKey(id string, layer, index int, act ButtonAction) error {
	bindMu.Lock()
	defer bindMu.Unlock()
	d, err := keyboards.get(id)
	if err != nil {
		return err
	}
	if layer < 0 || layer >= leobog.Layers || !layoutIndex(d, index) || !d.CanRemap() {
		return errors.New(tr("err.buttonAction"))
	}
	find := func() int {
		return slices.IndexFunc(cfg.Bindings, func(b Binding) bool { return b.Family == d.Name && b.Button == index && b.Layer == layer })
	}
	if act.Kind == "macro" {
		cfgMu.Lock()
		if !slices.ContainsFunc(cfg.Macros, func(m Macro) bool { return m.ID == act.Macro }) {
			cfgMu.Unlock()
			return errors.New(tr("err.macroMissing"))
		}
		if i := find(); i >= 0 {
			cfg.Bindings[i].Macro = act.Macro
		} else if trig := freeTrigger(cfg.Bindings); trig != 0 {
			cfg.Bindings = append(cfg.Bindings, Binding{Family: d.Name, Button: index, Layer: layer, Macro: act.Macro, Trigger: trig})
		} else {
			cfgMu.Unlock()
			return errors.New(tr("err.noTrigger", numTriggers))
		}
		cfgMu.Unlock()
	} else {
		a, err := actionFor(act, d.DefaultLayers()[layer][index])
		if err != nil {
			return err
		}
		cfgMu.Lock()
		if i := find(); i >= 0 {
			cfg.Bindings = slices.Delete(cfg.Bindings, i, i+1)
		}
		cfgMu.Unlock()
		km := keymapOf(d, true)
		km[layer][index] = a
		if err := rememberKeyboard(d.ID, func(m *KeyboardMemory) {
			if km == d.DefaultLayers() {
				m.Keymap = nil
			} else {
				m.Keymap = &km
			}
		}); err != nil {
			return err
		}
	}
	refreshHooks()
	syncKeymaps(false)
	return saveConfig()
}

// ResetKeys gives every key its factory action and unbinds its macros.
func (Keyboards) ResetKeys(id string) error {
	bindMu.Lock()
	defer bindMu.Unlock()
	d, err := keyboards.get(id)
	if err != nil {
		return err
	}
	cfgMu.Lock()
	cfg.Bindings = slices.DeleteFunc(cfg.Bindings, func(b Binding) bool { return b.Family == d.Name })
	cfgMu.Unlock()
	if err := rememberKeyboard(d.ID, func(m *KeyboardMemory) { m.Keymap = nil }); err != nil {
		return err
	}
	refreshHooks()
	// Written even if the app thinks it is there: the official driver may have changed it.
	if err := deviceSetKeymap(d, d.DefaultLayers()); err != nil {
		return err
	}
	keyboards.mu.Lock()
	keyboards.written[d.ID] = nil
	keyboards.mu.Unlock()
	return saveConfig()
}

// Reset restores the keyboard's factory keys and lighting, and unbinds its macros.
func (Keyboards) Reset(id string) error {
	bindMu.Lock()
	defer bindMu.Unlock()
	d, err := keyboards.get(id)
	if err != nil {
		return err
	}
	if err := deviceReset(d); err != nil {
		keyboards.mu.Lock()
		keyboards.written[d.ID] = &unknownKeymap
		keyboards.mu.Unlock()
		return err
	}
	cfgMu.Lock()
	cfg.Bindings = slices.DeleteFunc(cfg.Bindings, func(b Binding) bool { return b.Family == d.Name })
	cfgMu.Unlock()
	refreshHooks()
	keyboards.mu.Lock()
	keyboards.written[d.ID] = nil
	keyboards.mu.Unlock()
	colors, keys := d.Colors(), d.KeyColors()
	if err := rememberKeyboard(d.ID, func(m *KeyboardMemory) {
		m.Keymap, m.Colors, m.KeyColors = nil, colors, keys
	}); err != nil {
		return err
	}
	return saveConfig()
}

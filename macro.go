package main

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"opensynapse/internal/razer"
)

// Macros run on the PC. While OpenSynapse runs, a bound mouse button is
// remapped on the device to an otherwise unused key (F13–F24); the keyboard
// hook swallows that key and plays the macro with SendInput. On quit the
// buttons get their original function back. The DeathAdder V4 Pro has no
// macro memory, and this works on any device that takes a key assignment.

// MacroEvent is one input, sent Delay ms after the previous one.
type MacroEvent struct {
	Kind  string `json:"kind"` // key (Code: virtual-key code) or mouse (Code: 1 left, 2 right, 3 middle, 4 back, 5 forward)
	Code  int    `json:"code"`
	Down  bool   `json:"down"`
	Delay int    `json:"delay"`
}

// Macro is a recorded or edited input sequence.
type Macro struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Events []MacroEvent `json:"events"`
	Mode   string       `json:"mode"`  // once, repeat (Count times), toggle (until pressed again), hold (while held)
	Count  int          `json:"count"` // for repeat
}

// Binding plays a macro from a device button, which sends Trigger.
type Binding struct {
	Family   string           `json:"family"` // device family: cable and receiver share a binding
	Button   int              `json:"button"`
	Macro    string           `json:"macro"`
	Trigger  int              `json:"trigger"`  // virtual-key code, F13–F24
	Original razer.Assignment `json:"original"` // restored on unbind
}

const (
	firstTrigger = 0x7C // VK_F13
	numTriggers  = 12   // F13–F24
	leftButton   = 1
)

var macroModes = []string{"once", "repeat", "toggle", "hold"}

func isTriggerVK(vk int) bool { return vk >= firstTrigger && vk < firstTrigger+numTriggers }

// triggerAssignment makes a button send the trigger key (HID usage of F13 is 0x68).
func triggerAssignment(vk int) razer.Assignment {
	return razer.Assignment{Fn: razer.FnKey, Data: []byte{0, byte(0x68 + vk - firstTrigger)}}
}

func isTriggerAssignment(a razer.Assignment) bool {
	return a.Fn == razer.FnKey && len(a.Data) == 2 && a.Data[1] >= 0x68 && a.Data[1] < 0x68+numTriggers
}

// defaultAssignment stands in for an original lost with an old config.
// shortcut: only the DeathAdder V4 Pro defaults are known (its DPI button 0x60 cycles DPI); read the defaults from the device table once Synapse's are decoded.
func defaultAssignment(button int) razer.Assignment {
	if button == 0x60 {
		return razer.Assignment{Fn: 6, Data: []byte{6}}
	}
	return razer.Assignment{Fn: razer.FnMouse, Data: []byte{byte(button)}}
}

// freeTrigger returns an F13–F24 key no binding uses, or 0.
func freeTrigger(bs []Binding) int {
	for vk := firstTrigger; vk < firstTrigger+numTriggers; vk++ {
		if !slices.ContainsFunc(bs, func(b Binding) bool { return b.Trigger == vk }) {
			return vk
		}
	}
	return 0
}

func (m *Macro) normalize() error {
	m.Name = strings.TrimSpace(m.Name)
	if m.Name == "" {
		return errors.New(tr("err.macroName"))
	}
	if !slices.Contains(macroModes, m.Mode) {
		m.Mode = "once"
	}
	if m.Mode == "repeat" && (m.Count < 1 || m.Count > 999) {
		return errors.New(tr("err.macroCount", m.Name))
	}
	if len(m.Events) > 2000 {
		return errors.New(tr("err.macroLong", m.Name))
	}
	if m.Events == nil {
		m.Events = []MacroEvent{}
	}
	for _, e := range m.Events {
		bad := e.Delay < 0 || e.Delay > 600_000
		switch e.Kind {
		case "key":
			bad = bad || e.Code < 1 || e.Code > 254
		case "mouse":
			bad = bad || e.Code < 1 || e.Code > 5
		default:
			bad = true
		}
		if bad {
			return errors.New(tr("err.macroEvent", m.Name))
		}
	}
	return nil
}

// ---------- playback ----------

// inputKey names a key or mouse button, to track what is held down.
type inputKey struct {
	kind string
	code int
}

var (
	playMu  sync.Mutex
	playing = map[int]chan struct{}{} // by trigger: closing stops the run
	held    = map[int]bool{}          // trigger keys down; key repeat sends more downs
)

func macroForTrigger(vk int) (Macro, bool) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	for _, b := range cfg.Bindings {
		if b.Trigger == vk {
			for _, m := range cfg.Macros {
				if m.ID == b.Macro {
					return m, true
				}
			}
		}
	}
	return Macro{}, false
}

// onTriggerKey runs from the keyboard hook, so it only starts or stops a run.
// It reports whether the key was a bound trigger (and must be swallowed).
func onTriggerKey(vk int, down bool) bool {
	if !isTriggerVK(vk) {
		return false
	}
	m, ok := macroForTrigger(vk)
	if !ok {
		return false
	}
	playMu.Lock()
	defer playMu.Unlock()
	if held[vk] == down {
		return true // key repeat
	}
	held[vk] = down
	stop, running := playing[vk]
	switch {
	case down && running && m.Mode == "toggle", !down && running && m.Mode == "hold":
		close(stop)
		delete(playing, vk)
	case down && !running:
		stop = make(chan struct{})
		playing[vk] = stop
		go func() {
			play(m, stop)
			playMu.Lock()
			if playing[vk] == stop {
				delete(playing, vk)
			}
			playMu.Unlock()
		}()
	}
	return true
}

// play sends the events once, Count times, or until stopped (toggle, hold).
// Whatever is still down at the end is released, so no key stays stuck.
func play(m Macro, stop <-chan struct{}) {
	down := map[inputKey]MacroEvent{}
	defer func() {
		for _, e := range down {
			e.Down = false
			sendEvent(e)
		}
	}()
	runs := 1
	switch m.Mode {
	case "repeat":
		runs = m.Count
	case "toggle", "hold":
		runs = -1
	}
	wait := func(ms int) bool {
		if ms <= 0 {
			select {
			case <-stop:
				return false
			default:
				return true
			}
		}
		t := time.NewTimer(time.Duration(ms) * time.Millisecond)
		defer t.Stop()
		select {
		case <-stop:
			return false
		case <-t.C:
			return true
		}
	}
	for i := 0; runs < 0 || i < runs; i++ {
		total := 0
		for _, e := range m.Events {
			if !wait(e.Delay) {
				return
			}
			total += e.Delay
			sendEvent(e)
			k := inputKey{e.Kind, e.Code}
			if e.Down {
				down[k] = e
			} else {
				delete(down, k)
			}
		}
		// A repeat without delays would flood the input queue.
		if (runs < 0 || i+1 < runs) && total < 10 && !wait(10-total) {
			return
		}
	}
}

// ---------- recording ----------

var rec struct {
	sync.Mutex
	on     bool
	last   uint32 // hook timestamp of the previous event, ms
	down   map[inputKey]bool
	events []MacroEvent
}

// recordInput runs from the hooks.
func recordInput(e MacroEvent, at uint32) {
	rec.Lock()
	defer rec.Unlock()
	if !rec.on {
		return
	}
	k := inputKey{e.Kind, e.Code}
	if e.Down && rec.down[k] {
		return // key repeat
	}
	rec.down[k] = e.Down
	if len(rec.events) > 0 {
		e.Delay = int(at - rec.last)
	}
	rec.last = at
	rec.events = append(rec.events, e)
}

func hooksWanted() (keyboard, mouse bool) {
	rec.Lock()
	recording := rec.on
	rec.Unlock()
	cfgMu.Lock()
	bound := len(cfg.Bindings) > 0
	cfgMu.Unlock()
	return bound || recording, recording
}

// cleanRecording drops the click on the stop button (the last left down/up)
// and releases that have no press in the recording.
func cleanRecording(ev []MacroEvent) []MacroEvent {
	if n := len(ev); n >= 2 && ev[n-2] == (MacroEvent{"mouse", leftButton, true, ev[n-2].Delay}) && ev[n-1].Kind == "mouse" && ev[n-1].Code == leftButton && !ev[n-1].Down {
		ev = ev[:n-2]
	}
	out := []MacroEvent{}
	down := map[inputKey]bool{}
	for _, e := range ev {
		k := inputKey{e.Kind, e.Code}
		if !e.Down && !down[k] {
			continue
		}
		down[k] = e.Down
		out = append(out, e)
	}
	if len(out) > 0 {
		out[0].Delay = 0
	}
	return out
}

// ---------- bound API ----------

// Macros records, edits and stores macros.
type Macros struct{}

// List returns the saved macros.
func (Macros) List() []Macro {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return slices.Clone(cfg.Macros)
}

// Save stores a macro; one without an ID is new. It returns the stored macro.
func (Macros) Save(m Macro) (Macro, error) {
	if err := m.normalize(); err != nil {
		return m, err
	}
	cfgMu.Lock()
	if i := slices.IndexFunc(cfg.Macros, func(x Macro) bool { return x.ID == m.ID }); m.ID != "" && i >= 0 {
		cfg.Macros[i] = m
	} else {
		m.ID = fmt.Sprintf("%x", time.Now().UnixNano())
		cfg.Macros = append(cfg.Macros, m)
	}
	cfgMu.Unlock()
	return m, saveConfig()
}

// Delete removes a macro that no button plays.
func (Macros) Delete(id string) error {
	cfgMu.Lock()
	if slices.ContainsFunc(cfg.Bindings, func(b Binding) bool { return b.Macro == id }) {
		cfgMu.Unlock()
		return errors.New(tr("err.macroBound"))
	}
	cfg.Macros = slices.DeleteFunc(cfg.Macros, func(m Macro) bool { return m.ID == id })
	cfgMu.Unlock()
	return saveConfig()
}

// StartRecording captures keys and mouse buttons until StopRecording.
func (Macros) StartRecording() {
	rec.Lock()
	rec.on, rec.events, rec.down = true, nil, map[inputKey]bool{}
	rec.Unlock()
	refreshHooks()
}

// StopRecording returns what was recorded.
func (Macros) StopRecording() []MacroEvent {
	rec.Lock()
	ev := rec.events
	rec.on, rec.events = false, nil
	rec.Unlock()
	refreshHooks()
	return cleanRecording(ev)
}

// ButtonInfo is a remappable button and the macro it plays ("" for none).
type ButtonInfo struct {
	ID    int    `json:"id"`
	Macro string `json:"macro"`
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
	cfgMu.Lock()
	defer cfgMu.Unlock()
	for _, b := range ids {
		info := ButtonInfo{ID: b}
		for _, bd := range cfg.Bindings {
			if bd.Family == d.Family() && bd.Button == b {
				info.Macro = bd.Macro
			}
		}
		out = append(out, info)
	}
	return out, nil
}

var bindMu sync.Mutex // one bind at a time, so two never take the same trigger

var bindingsKick = make(chan struct{}, 1)

func kickBindings() {
	select {
	case bindingsKick <- struct{}{}:
	default:
	}
}

// keepBindings makes bound buttons send their trigger: at start, after a
// device change, and every 10 s. Only the live profile takes the mapping, and
// the app restores the buttons when it quits, so a mouse that slept, was
// unplugged or used elsewhere gets it back here.
func keepBindings() {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		applyBindings(false)
		select {
		case <-t.C:
		case <-bindingsKick:
		}
	}
}

// applyBindings writes every bound button of the connected devices that
// differs: its trigger key, or with restore its original function. A device
// that does not answer (asleep) is skipped until the next round.
func applyBindings(restore bool) {
	bindMu.Lock()
	defer bindMu.Unlock()
	cfgMu.Lock()
	bs := slices.Clone(cfg.Bindings)
	cfgMu.Unlock()
	if len(bs) == 0 {
		return
	}
	for _, d := range devices.list() {
		for _, b := range bs {
			if b.Family != d.Family() {
				continue
			}
			want := triggerAssignment(b.Trigger)
			if restore {
				want = b.Original
			}
			cur, err := d.ButtonAssignment(b.Button)
			if err != nil {
				break // asleep or gone: skip the device
			}
			if cur.Fn == want.Fn && slices.Equal(cur.Data, want.Data) {
				continue
			}
			if err := d.SetButtonAssignment(b.Button, want); err != nil {
				log.Println("button", b.Button, "of", d.ID, err)
			}
		}
	}
}

// BindButton makes a button play a macro; an empty macro restores the button.
// The left button cannot be bound: if OpenSynapse is not running it would
// only send F13, leaving no way to click.
func (Devices) BindButton(id string, button int, macro string) error {
	bindMu.Lock()
	defer bindMu.Unlock()
	d, err := devices.get(id)
	if err != nil {
		return err
	}
	if button == leftButton {
		return errors.New(tr("err.leftButton"))
	}
	fam := d.Family()
	find := func() int {
		return slices.IndexFunc(cfg.Bindings, func(b Binding) bool { return b.Family == fam && b.Button == button })
	}
	cfgMu.Lock()
	i := find()
	if macro == "" {
		if i < 0 {
			cfgMu.Unlock()
			return nil
		}
		orig := cfg.Bindings[i].Original
		cfgMu.Unlock()
		if err := d.SetButtonAssignment(button, orig); err != nil {
			return err
		}
		cfgMu.Lock()
		if i := find(); i >= 0 {
			cfg.Bindings = slices.Delete(cfg.Bindings, i, i+1)
		}
		cfgMu.Unlock()
		refreshHooks()
		return saveConfig()
	}
	if !slices.ContainsFunc(cfg.Macros, func(m Macro) bool { return m.ID == macro }) {
		cfgMu.Unlock()
		return errors.New(tr("err.macroMissing"))
	}
	if i >= 0 { // already sends a trigger: only the macro changes
		cfg.Bindings[i].Macro = macro
		cfgMu.Unlock()
		return saveConfig()
	}
	trig := freeTrigger(cfg.Bindings)
	cfgMu.Unlock()
	if trig == 0 {
		return errors.New(tr("err.noTrigger", numTriggers))
	}
	orig, err := d.ButtonAssignment(button)
	if err != nil {
		return err
	}
	if isTriggerAssignment(orig) {
		orig = defaultAssignment(button)
	}
	if err := d.SetButtonAssignment(button, triggerAssignment(trig)); err != nil {
		return err
	}
	cfgMu.Lock()
	cfg.Bindings = append(cfg.Bindings, Binding{Family: fam, Button: button, Macro: macro, Trigger: trig, Original: orig})
	cfgMu.Unlock()
	refreshHooks()
	return saveConfig()
}

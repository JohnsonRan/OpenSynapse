package main

import (
	"sync"
	"time"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

// Keyboard notices: keyboards that opt in flash all their keys briefly when
// something happens. Flashes are frames, which the keyboard does not store:
// it shows its own lighting again about a second after the last one.

// Notice colors and flash counts.
var (
	noticeBattery = notice{razer.RGB{R: 0xff, G: 0x20}, 3}          // a mouse runs low
	noticeScene   = notice{razer.RGB{R: 0x44, G: 0xd6, B: 0x2c}, 1} // another scene takes over
	noticeMacro   = notice{razer.RGB{R: 0xff, G: 0xff, B: 0xff}, 1} // a toggled macro starts or stops
)

type notice struct {
	color razer.RGB
	times int
}

const flashStep = 140 * time.Millisecond

var flashing = struct {
	sync.Mutex
	until time.Time // music frames wait until then
	busy  bool
}{}

// noticeShowing reports whether a notice is on the keyboards; music waits for it.
func noticeShowing() bool {
	flashing.Lock()
	defer flashing.Unlock()
	return time.Now().Before(flashing.until)
}

// flashKeyboards shows a notice on the keyboards that want notices. A notice
// arriving during another is dropped.
func flashKeyboards(n notice) {
	var kbs []*leobog.Device
	for _, d := range keyboards.list() {
		if keyboardMemory(d.ID).Notices {
			kbs = append(kbs, d)
		}
	}
	flashing.Lock()
	if len(kbs) == 0 || flashing.busy {
		flashing.Unlock()
		return
	}
	flashing.busy = true
	flashing.until = time.Now().Add(time.Duration(2*n.times) * flashStep)
	flashing.Unlock()
	go func() {
		defer func() { flashing.Lock(); flashing.busy = false; flashing.Unlock() }()
		var on, off leobog.KeyColors
		for i := range on {
			on[i] = n.color
		}
		// Off between flashes, and ending on: the last flash stays until the keyboard returns to its lighting.
		for i := range 2*n.times - 1 {
			frame := &on
			if i%2 == 1 {
				frame = &off
			}
			for _, d := range kbs {
				_ = d.Frame(frame)
			}
			time.Sleep(flashStep)
		}
	}()
}

// SetNotices turns keyboard notices on or off for a keyboard.
func (Keyboards) SetNotices(id string, on bool) error {
	d, err := keyboards.get(id)
	if err != nil {
		return err
	}
	return rememberKeyboard(d.ID, func(m *KeyboardMemory) { m.Notices = on })
}

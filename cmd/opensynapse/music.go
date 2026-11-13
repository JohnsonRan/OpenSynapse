package main

import (
	"errors"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
	"github.com/JohnsonRan/OpenSynapse/internal/lightshow"
	"github.com/JohnsonRan/OpenSynapse/internal/razer"
	"github.com/JohnsonRan/OpenSynapse/internal/winaudio"
)

// Music lighting: while a keyboard has a music effect, what the PC plays is
// drawn on its keys as frames, about 21 a second like the official driver.
// Frames only show while they keep coming: after a second of silence no
// frames go out, and the keyboard shows its own effect again.

const frameEvery = 47 * time.Millisecond

var musicKick = make(chan struct{}, 1)

func kickMusic() {
	select {
	case musicKick <- struct{}{}:
	default:
	}
}

// musicKeyboard is a connected keyboard with a music effect.
type musicKeyboard struct {
	d      *leobog.Device
	effect string
	color  razer.RGB
}

// musicKeyboards appends the connected keyboards with a music effect to buf.
// The painter asks before every frame and passes its last answer's storage.
func musicKeyboards(buf []musicKeyboard) []musicKeyboard {
	for _, d := range keyboards.list() {
		if m := keyboardMemory(d.ID); m.Music != "" {
			buf = append(buf, musicKeyboard{d, m.Music, m.MusicColor})
		}
	}
	return buf
}

// keyPlaces are a model's key centres for the painter.
func keyPlaces(m *leobog.Model) []lightshow.Key {
	out := make([]lightshow.Key, len(m.Layout))
	for i, k := range m.Layout {
		out[i] = lightshow.Key{Index: k.Index, X: (float64(k.X) + float64(k.W)/2) / float64(m.Width), Y: (float64(k.Y) + float64(k.H)/2) / float64(m.Height)}
	}
	return out
}

// runMusic listens to the PC only while a keyboard wants it.
func runMusic() {
	for {
		if len(musicKeyboards(nil)) == 0 {
			<-musicKick
			continue
		}
		if !guard("music lighting", playMusic) {
			time.Sleep(captureRetry)
		}
	}
}

// Tests replace the sound and the keyboard.
var (
	loopback    = winaudio.Loopback
	deviceFrame = (*leobog.Device).Frame
)

// captureRetry is how long music lighting waits before it listens again after
// a failure: a playback device that went away does not come back sooner.
const captureRetry = 2 * time.Second

// playMusic draws the sound until no keyboard has a music effect.
func playMusic() {
	var mu sync.Mutex
	var an lightshow.Analyzer
	fresh := false
	heard := make(chan struct{}, 1) // wakes the painter when sound arrives
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			var err error
			ok := guard("music capture", func() {
				err = loopback(stop, func(mono []float32, rate int) {
					mu.Lock()
					defer mu.Unlock() // a panic must not leave the painter waiting on mu
					an.Add(mono, rate)
					fresh = true
					select {
					case heard <- struct{}{}:
					default:
					}
				})
			})
			select {
			case <-stop:
				return
			default:
			}
			if err != nil {
				log.Println("music lighting:", err)
			}
			if err != nil || !ok {
				select { // no playback device, or it went away: try again
				case <-stop:
					return
				case <-time.After(captureRetry):
				}
			}
		}
	}()
	// levels takes what the capture analyzed since the last frame, if anything.
	levels := func() (lightshow.Levels, bool) {
		mu.Lock()
		defer mu.Unlock()
		select {
		case <-heard: // taken with the sound it announced
		default:
		}
		if !fresh {
			return lightshow.Levels{}, false
		}
		fresh = false
		return an.Levels(), true
	}
	defer func() { close(stop); <-done }()
	t := time.NewTicker(frameEvery)
	defer t.Stop()
	painters := map[string]*lightshow.Painter{}
	places := map[*leobog.Model][]lightshow.Key{}
	colors := map[int]razer.RGB{}
	var kbs []musicKeyboard
	var quietSince time.Time
	for {
		if kbs = musicKeyboards(kbs[:0]); len(kbs) == 0 {
			return
		}
		l, ok := levels()
		if !ok {
			// Nothing plays, and Windows sends nothing: there is no frame to
			// draw until sound arrives or a keyboard's effect changes.
			select {
			case <-heard:
			case <-musicKick:
			}
			continue
		}
		draw := !noticeShowing()
		if draw {
			if l.Loud > 0.02 || l.Bass > 0.02 {
				quietSince = time.Time{}
			} else if quietSince.IsZero() {
				quietSince = time.Now()
			} else if time.Since(quietSince) > time.Second {
				draw = false // a pause, not a quiet passage
			}
		}
		for _, kb := range kbs {
			if !draw {
				break // the frames stop, and the keyboard shows its own effect
			}
			p := painters[kb.d.ID]
			if p == nil {
				p = &lightshow.Painter{}
				painters[kb.d.ID] = p
			}
			if places[kb.d.Model] == nil {
				places[kb.d.Model] = keyPlaces(kb.d.Model)
			}
			p.Effect, p.Color = kb.effect, kb.color
			clear(colors)
			p.Paint(l, places[kb.d.Model], colors)
			var frame leobog.KeyColors
			for i, c := range colors {
				frame[i] = c
			}
			if err := deviceFrame(kb.d, &frame); err != nil {
				log.Println("music frame:", err)
			}
		}
		select { // the next frame's time
		case <-t.C:
		case <-musicKick:
		}
	}
}

// SetMusic draws what the PC plays on the keyboard with an effect
// (pulse, spectrum, bass), or with "" stops it. Color is pulse's.
func (Keyboards) SetMusic(id, effect string, color razer.RGB) error {
	if effect != "" && !slices.Contains(lightshow.Effects, effect) {
		return errors.New(tr("err.musicEffect"))
	}
	d, err := keyboards.get(id)
	if err != nil {
		return err
	}
	err = rememberKeyboard(d.ID, func(m *KeyboardMemory) { m.Music, m.MusicColor = effect, color })
	kickMusic()
	return err
}

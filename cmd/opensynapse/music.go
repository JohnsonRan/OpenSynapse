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

// musicKeyboards are the connected keyboards with a music effect, and the effect.
func musicKeyboards() map[*leobog.Device]KeyboardMemory {
	out := map[*leobog.Device]KeyboardMemory{}
	for _, d := range keyboards.list() {
		if m := keyboardMemory(d.ID); m.Music != "" {
			out[d] = m
		}
	}
	return out
}

// keyPlaces are the Hi75's key centres for the painter.
var keyPlaces = func() []lightshow.Key {
	out := make([]lightshow.Key, len(leobog.Layout))
	for i, k := range leobog.Layout {
		out[i] = lightshow.Key{Index: k.Index, X: (float64(k.X) + float64(k.W)/2) / leobog.LayoutW, Y: (float64(k.Y) + float64(k.H)/2) / leobog.LayoutH}
	}
	return out
}()

// runMusic listens to the PC only while a keyboard wants it.
func runMusic() {
	for {
		if len(musicKeyboards()) == 0 {
			<-musicKick
			continue
		}
		playMusic()
	}
}

// playMusic draws the sound until no keyboard has a music effect.
func playMusic() {
	var mu sync.Mutex
	var an lightshow.Analyzer
	fresh := false
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			err := winaudio.Loopback(stop, func(mono []float32, rate int) {
				mu.Lock()
				an.Add(mono, rate)
				fresh = true
				mu.Unlock()
			})
			select {
			case <-stop:
				return
			default:
			}
			if err != nil {
				log.Println("music lighting:", err)
				select { // no playback device, or it went away: try again
				case <-stop:
					return
				case <-time.After(2 * time.Second):
				}
			}
		}
	}()
	defer func() { close(stop); <-done }()
	t := time.NewTicker(frameEvery)
	defer t.Stop()
	painters := map[string]*lightshow.Painter{}
	colors := map[int]razer.RGB{}
	var quietSince time.Time
	for {
		select {
		case <-t.C:
		case <-musicKick:
		}
		kbs := musicKeyboards()
		if len(kbs) == 0 {
			return
		}
		mu.Lock()
		if !fresh {
			mu.Unlock()
			continue
		}
		fresh = false
		l := an.Levels()
		mu.Unlock()
		if l.Loud > 0.02 || l.Bass > 0.02 {
			quietSince = time.Time{}
		} else if quietSince.IsZero() {
			quietSince = time.Now()
		} else if time.Since(quietSince) > time.Second {
			continue // a pause, not a quiet passage
		}
		for d, m := range kbs {
			p := painters[d.ID]
			if p == nil {
				p = &lightshow.Painter{}
				painters[d.ID] = p
			}
			p.Effect, p.Color = m.Music, m.MusicColor
			clear(colors)
			p.Paint(l, keyPlaces, colors)
			var frame leobog.KeyColors
			for i, c := range colors {
				frame[i] = c
			}
			if err := d.Frame(&frame); err != nil {
				log.Println("music frame:", err)
			}
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

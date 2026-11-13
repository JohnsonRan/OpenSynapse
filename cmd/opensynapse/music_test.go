package main

import (
	"math"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
)

// Music lighting draws frames while the PC plays, and while nothing plays
// its painter sleeps: no frame, and no wake-up for the next frame's time.
func TestMusicDrawsSoundAndSleepsInSilence(t *testing.T) {
	d, _ := fakeKeyboard(t, map[string]*[leobog.Layers]leobog.Layer{})
	cfgMu.Lock()
	cfg.Keyboards = map[string]KeyboardMemory{d.ID: {Music: "pulse"}}
	cfgMu.Unlock()
	origLoopback, origFrame := loopback, deviceFrame
	var frames atomic.Int32
	deviceFrame = func(*leobog.Device, *leobog.KeyColors) error { frames.Add(1); return nil }
	const sound = 60 * time.Millisecond
	play, played := make(chan struct{}), make(chan struct{})
	loopback = func(stop <-chan struct{}, fn func([]float32, int)) error {
		packet := make([]float32, 480) // 10 ms of a 100 Hz tone at 48 kHz
		for i := range packet {
			packet[i] = float32(math.Sin(2 * math.Pi * 100 * float64(i) / 48000))
		}
		select {
		case <-play:
		case <-stop:
			return nil
		}
		for end := time.Now().Add(sound); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			fn(packet, 48000)
		}
		close(played)
		<-stop
		return nil
	}
	done := make(chan struct{})
	go func() { playMusic(); close(done) }()
	t.Cleanup(func() {
		cfgMu.Lock()
		cfg.Keyboards[d.ID] = KeyboardMemory{}
		cfgMu.Unlock()
		kickMusic()
		<-done
		loopback, deviceFrame = origLoopback, origFrame
	})

	time.Sleep(10 * time.Millisecond) // the painter finds nothing to draw
	const quiet = 2*frameEvery + 10*time.Millisecond
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	time.Sleep(quiet)
	runtime.ReadMemStats(&after)
	// A painter that wakes at every frame time asks for the keyboards, one
	// allocation each time: two in this time. One is left for whatever else runs.
	if n := after.Mallocs - before.Mallocs; n > 1 || frames.Load() != 0 {
		t.Fatalf("%d allocations and %d frames in %v of silence, want none", n, frames.Load(), quiet)
	}

	close(play)
	<-played
	if drawn, most := frames.Load(), int32(sound/frameEvery)+3; drawn < 2 || drawn > most {
		t.Fatalf("%d frames for %v of sound, want 2 to %d", drawn, sound, most)
	}
}

// What the painter asks for before every frame must not allocate once its
// storage has grown.
func TestMusicKeyboardsReusesStorage(t *testing.T) {
	d, _ := fakeKeyboard(t, map[string]*[leobog.Layers]leobog.Layer{})
	cfgMu.Lock()
	cfg.Keyboards = map[string]KeyboardMemory{d.ID: {Music: "pulse"}}
	cfgMu.Unlock()
	kbs := musicKeyboards(nil)
	if len(kbs) != 1 || kbs[0].d != d || kbs[0].effect != "pulse" {
		t.Fatalf("%+v", kbs)
	}
	// keyboards.list copies the device list: one allocation.
	if n := testing.AllocsPerRun(100, func() { kbs = musicKeyboards(kbs[:0]) }); n > 1 {
		t.Fatalf("%v allocations per frame, want the device list's one", n)
	}
}

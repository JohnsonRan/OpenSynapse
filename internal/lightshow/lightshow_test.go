package lightshow

import (
	"math"
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

func tone(freq, amp float64, n, rate int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(amp * math.Sin(2*math.Pi*freq*float64(i)/float64(rate)))
	}
	return out
}

func TestFFTFindsTone(t *testing.T) {
	x := make([]complex128, 64)
	for i := range x {
		x[i] = complex(math.Cos(2*math.Pi*8*float64(i)/64), 0)
	}
	fft(x)
	for i := 1; i < 32; i++ {
		if i != 8 && real(x[i]*complexConj(x[i])) > 1e-9 {
			t.Fatalf("energy in bin %d", i)
		}
	}
	if math.Abs(real(x[8])-32) > 1e-9 {
		t.Fatalf("bin 8 = %v", x[8])
	}
}

func complexConj(c complex128) complex128 { return complex(real(c), -imag(c)) }

func TestBassToneLightsLeftBars(t *testing.T) {
	var a Analyzer
	a.Add(tone(80, 0.5, 4096, 48000), 48000)
	l := a.Levels()
	if l.Bands[1] < 0.9 && l.Bands[2] < 0.9 && l.Bands[3] < 0.9 {
		t.Fatalf("80 Hz did not fill a low bar: %v", l.Bands)
	}
	if l.Bands[Bands-1] > 0.3 || l.Bass < 0.9 {
		t.Fatalf("levels %+v", l)
	}
	// Steady music shows dim; a beat twice as loud shows full.
	for range 100 {
		a.Add(tone(80, 0.2, 1024, 48000), 48000)
		l = a.Levels()
	}
	steady := l.Loud
	a.Add(tone(80, 0.4, 1024, 48000), 48000)
	if l = a.Levels(); steady > 0.35 || l.Loud < 0.9 {
		t.Fatalf("steady %.2f, beat %.2f", steady, l.Loud)
	}
	// Silence fades out instead of being normalized up.
	for range 40 {
		a.Add(make([]float32, 1024), 48000)
		l = a.Levels()
	}
	if l.Loud > 0.01 || l.Bass > 0.01 {
		t.Fatalf("silence: %+v", l)
	}
}

func TestPaint(t *testing.T) {
	keys := []Key{{Index: 0, X: 0.01, Y: 0.95}, {Index: 1, X: 0.01, Y: 0.05}, {Index: 2, X: 0.99, Y: 0.95}}
	out := map[int]razer.RGB{}
	var l Levels
	l.Bands[0] = 0.5 // the left bar half up: its bottom key lit, its top key dark
	(&Painter{Effect: Spectrum}).Paint(l, keys, out)
	if out[0] == (razer.RGB{}) || out[1] != (razer.RGB{}) || out[2] != (razer.RGB{}) {
		t.Fatalf("spectrum: %v", out)
	}
	(&Painter{Effect: Pulse, Color: razer.RGB{R: 200}}).Paint(Levels{Loud: 0.5}, keys, out)
	if out[1] != (razer.RGB{R: 100}) {
		t.Fatalf("pulse: %v", out[1])
	}
	p := &Painter{Effect: Bass}
	p.Paint(Levels{Bass: 1}, keys, out)
	first := out[0]
	p.Paint(Levels{Bass: 0}, keys, out)
	p.Paint(Levels{Bass: 1}, keys, out)
	if out[0] == first || out[0] == (razer.RGB{}) {
		t.Fatal("a new bass hit kept the color")
	}
}

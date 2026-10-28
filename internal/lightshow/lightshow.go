// Package lightshow turns what the PC plays into key colors: it measures
// loudness and a spectrum, and paints them on a keyboard's key layout.
package lightshow

import (
	"math"
	"math/cmplx"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

// Effects, by name.
const (
	Pulse    = "pulse"    // every key in one color, as bright as the music is loud
	Spectrum = "spectrum" // columns of keys as frequency bars, bass on the left
	Bass     = "bass"     // a flash of a new color on each bass hit
)

// Effects lists the effects in the order the page offers them.
var Effects = []string{Pulse, Spectrum, Bass}

// Bands is the number of spectrum bars.
const Bands = 16

const fftSize = 1024

// Levels is one measurement, each value 0–1.
type Levels struct {
	Loud  float64
	Bass  float64
	Bands [Bands]float64
}

// Analyzer keeps the latest samples and turns them into levels. Levels
// adapt to the music's own loudness, so quiet and loud songs both fill the range.
type Analyzer struct {
	buf      [fftSize]float64
	n        int // samples written into buf, wrapping
	rate     int
	loudAvg  float64        // loudness over the last couple of seconds
	bandPeak [Bands]float64 // slowly falling maxima for normalizing
	bassPeak float64
	out      Levels // smoothed
}

// Add appends samples in -1…1 at a sample rate.
func (a *Analyzer) Add(mono []float32, rate int) {
	a.rate = rate
	for _, v := range mono {
		a.buf[a.n%fftSize] = float64(v)
		a.n++
	}
}

// Levels measures the latest fftSize samples.
func (a *Analyzer) Levels() Levels {
	if a.rate == 0 {
		return Levels{}
	}
	x := make([]complex128, fftSize)
	var rms float64
	for i := range fftSize {
		v := a.buf[(a.n+i)%fftSize] // oldest first
		rms += v * v
		w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/(fftSize-1)) // Hann window
		x[i] = complex(v*w, 0)
	}
	rms = math.Sqrt(rms / fftSize)
	fft(x)
	mag := func(f float64) float64 { // magnitude at a frequency
		i := int(math.Round(f * fftSize / float64(a.rate)))
		if i < 1 || i >= fftSize/2 {
			return 0
		}
		return cmplx.Abs(x[i])
	}
	var raw Levels
	raw.Loud = rms
	// Log-spaced bars from 50 Hz to 12 kHz, each the strongest bin in its range.
	lo, hi := 50.0, 12000.0
	for b := range Bands {
		f0 := lo * math.Pow(hi/lo, float64(b)/Bands)
		f1 := lo * math.Pow(hi/lo, float64(b+1)/Bands)
		var m float64
		for f := f0; f < f1; f += float64(a.rate) / fftSize {
			m = math.Max(m, mag(f))
		}
		raw.Bands[b] = m
	}
	for f := 40.0; f < 150; f += float64(a.rate) / fftSize {
		raw.Bass = math.Max(raw.Bass, mag(f))
	}
	// Loudness against its recent average, so beats stand out: the average
	// shows dim, half again as loud is full.
	a.loudAvg = math.Max(a.loudAvg*0.97+raw.Loud*0.03, 0.005)
	raw.Loud = math.Max(0, math.Min(1, (raw.Loud/a.loudAvg-0.6)/0.9))
	raw.Loud *= raw.Loud
	a.bassPeak = norm(&raw.Bass, a.bassPeak, 0.5)
	for b := range Bands {
		a.bandPeak[b] = norm(&raw.Bands[b], a.bandPeak[b], 0.3)
	}
	a.out.Loud = smooth(a.out.Loud, raw.Loud)
	a.out.Bass = smooth(a.out.Bass, raw.Bass)
	for b := range Bands {
		a.out.Bands[b] = smooth(a.out.Bands[b], raw.Bands[b])
	}
	return a.out
}

// norm scales *v by a peak that follows it up at once and falls slowly; below
// floor the peak stops falling, so near-silence stays dark.
func norm(v *float64, peak, floor float64) float64 {
	peak = math.Max(math.Max(*v, peak*0.995), floor)
	*v = math.Min(*v/peak, 1)
	return peak
}

// smooth rises fast and falls slower, so lights snap on and fade out.
func smooth(old, v float64) float64 {
	if v > old {
		return v
	}
	return old*0.75 + v*0.25
}

// fft is an in-place radix-2 Cooley–Tukey transform; len(x) is a power of 2.
func fft(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wk := complex(1, 0)
			for k := range size / 2 {
				a, b := x[start+k], x[start+k+size/2]*wk
				x[start+k], x[start+k+size/2] = a+b, a-b
				wk *= w
			}
		}
	}
}

// Key is where a key sits, its centre as fractions of the keyboard: X 0 at
// the left, Y 0 at the top.
type Key struct {
	Index int
	X, Y  float64
}

// Painter draws levels on keys, one frame at a time.
type Painter struct {
	Effect string
	Color  razer.RGB // Pulse
	hue    float64   // Bass: the color of the current flash
	bassOn bool
}

// Paint colors each key for one frame; the result is by key index.
func (p *Painter) Paint(l Levels, keys []Key, out map[int]razer.RGB) {
	switch p.Effect {
	case Pulse:
		c := scale(p.Color, l.Loud)
		for _, k := range keys {
			out[k.Index] = c
		}
	case Spectrum:
		for _, k := range keys {
			b := min(int(k.X*Bands), Bands-1)
			height := 1 - k.Y // 0 at the bottom row, near 1 at the top
			if l.Bands[b] < height*0.9 {
				out[k.Index] = razer.RGB{}
				continue
			}
			out[k.Index] = hsv(120*(1-height), 1) // green at the bottom to red on top
		}
	case Bass:
		hit := l.Bass > 0.6
		if hit && !p.bassOn {
			p.hue = math.Mod(p.hue+67, 360) // a new color per hit
		}
		p.bassOn = hit
		c := hsv(p.hue, l.Bass*l.Bass)
		for _, k := range keys {
			out[k.Index] = c
		}
	}
}

func scale(c razer.RGB, f float64) razer.RGB {
	f = math.Max(0, math.Min(1, f))
	return razer.RGB{R: uint8(float64(c.R) * f), G: uint8(float64(c.G) * f), B: uint8(float64(c.B) * f)}
}

// hsv is a fully saturated color of a hue (degrees) at a brightness 0–1.
func hsv(h, v float64) razer.RGB {
	v = math.Max(0, math.Min(1, v))
	f := func(n float64) uint8 {
		k := math.Mod(n+h/60, 6)
		return uint8(255 * v * (1 - math.Max(0, math.Min(math.Min(k, 4-k), 1))))
	}
	return razer.RGB{R: f(5), G: f(3), B: f(1)}
}

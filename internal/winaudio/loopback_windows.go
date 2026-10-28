package winaudio

import (
	"encoding/binary"
	"math"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iidIAudioCaptureClient = windows.GUID{Data1: 0xC8ADBD64, Data2: 0xE71E, Data3: 0x48A0, Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
	procCoTaskMemFree      = ole32.NewProc("CoTaskMemFree")
)

// IAudioClient and IAudioCaptureClient slots.
const (
	slotInitialize        = 3
	slotGetMixFormat      = 8
	slotStart             = 10
	slotStop              = 11
	slotGetService        = 14
	slotGetBuffer         = 3
	slotReleaseBuffer     = 4
	slotGetNextPacketSize = 5

	streamLoopback = 0x00020000
	bufferSilent   = 0x2
)

// Loopback hands fn what the default playback device plays, mixed to mono,
// with its sample rate, until stop closes. It returns nil when the default
// device changes, so the caller starts again on the new one. While nothing
// plays Windows sends nothing, and fn is not called.
func Loopback(stop <-chan struct{}, fn func(mono []float32, rate int)) error {
	start, err := Default(Playback)
	if err != nil {
		return err
	}
	return withCOM(func() error {
		enum, err := create(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator)
		if err != nil {
			return err
		}
		defer release(enum)
		var dev, client, capture unsafe.Pointer
		if err := hrErr("GetDefaultAudioEndpoint", call(enum, slotGetDefault, uintptr(Playback), 0, uintptr(unsafe.Pointer(&dev)))); err != nil {
			return err
		}
		defer release(dev)
		if err := hrErr("Activate", call(dev, slotActivate, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client)))); err != nil {
			return err
		}
		defer release(client)
		var wfx unsafe.Pointer
		if err := hrErr("GetMixFormat", call(client, slotGetMixFormat, uintptr(unsafe.Pointer(&wfx)))); err != nil {
			return err
		}
		defer procCoTaskMemFree.Call(uintptr(wfx))
		raw := unsafe.Slice((*byte)(wfx), 18+int(*(*uint16)(unsafe.Add(wfx, 16))))
		f, err := parseWaveFormat(raw)
		if err != nil {
			return err
		}
		float := isFloat(raw)
		const bufferDuration = 2_000_000 // 200 ms, in 100 ns units
		if err := hrErr("Initialize", call(client, slotInitialize, 0, streamLoopback, bufferDuration, 0, uintptr(wfx), 0)); err != nil {
			return err
		}
		if err := hrErr("GetService", call(client, slotGetService, uintptr(unsafe.Pointer(&iidIAudioCaptureClient)), uintptr(unsafe.Pointer(&capture)))); err != nil {
			return err
		}
		defer release(capture)
		if err := hrErr("Start", call(client, slotStart)); err != nil {
			return err
		}
		defer call(client, slotStop)
		frameBytes := int(binary.LittleEndian.Uint16(raw[12:])) // nBlockAlign: every channel's container
		var mono []float32
		check := time.Now().Add(time.Second)
		for {
			select {
			case <-stop:
				return nil
			case <-time.After(10 * time.Millisecond):
			}
			if time.Now().After(check) {
				check = time.Now().Add(time.Second)
				if cur, err := Default(Playback); err != nil || cur != start {
					return err // another device plays now: start over on it
				}
			}
			for {
				var n uint32
				if hrErr("GetNextPacketSize", call(capture, slotGetNextPacketSize, uintptr(unsafe.Pointer(&n)))) != nil || n == 0 {
					break
				}
				var data unsafe.Pointer
				var frames, flags uint32
				if err := hrErr("GetBuffer", call(capture, slotGetBuffer, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&flags)), 0, 0)); err != nil {
					return err
				}
				mono = mono[:0]
				if flags&bufferSilent != 0 || data == nil {
					mono = append(mono, make([]float32, frames)...)
				} else {
					mono = mixDown(mono, unsafe.Slice((*byte)(data), int(frames)*frameBytes), f.channels, frameBytes, float)
				}
				call(capture, slotReleaseBuffer, uintptr(frames))
				fn(mono, f.format.Rate)
			}
		}
	})
}

// isFloat tells a 32-bit float mix format (what Windows uses) from integer PCM.
func isFloat(wfx []byte) bool {
	tag := binary.LittleEndian.Uint16(wfx)
	if tag == formatExtensible && len(wfx) >= 40 {
		return binary.LittleEndian.Uint32(wfx[24:]) == subtypeFloat.Data1
	}
	return tag == 3 // WAVE_FORMAT_IEEE_FLOAT
}

// mixDown averages each frame's channels into one sample in -1…1.
func mixDown(dst []float32, b []byte, channels, frameBytes int, float bool) []float32 {
	size := frameBytes / channels
	le := binary.LittleEndian
	for off := 0; off+frameBytes <= len(b); off += frameBytes {
		var sum float32
		for c := range channels {
			s := b[off+c*size : off+(c+1)*size]
			switch {
			case float && size == 4:
				sum += math.Float32frombits(le.Uint32(s))
			case size == 4:
				sum += float32(int32(le.Uint32(s))) / (1 << 31)
			case size == 3:
				sum += float32(int32(uint32(s[0])<<8|uint32(s[1])<<16|uint32(s[2])<<24)) / (1 << 31)
			case size == 2:
				sum += float32(int16(le.Uint16(s))) / (1 << 15)
			}
		}
		dst = append(dst, sum/float32(channels))
	}
	return dst
}

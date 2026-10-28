package winaudio

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Device is an active playback device, as Sound settings lists it.
type Device struct {
	ID       string `json:"id"`
	Name     string `json:"name"` // e.g. "Speakers (TP35 Pro)"
	instance string // hardware instance ID: "{1}.USB\VID_…&PID_…&MI_01\…"
}

// Devices lists the active playback devices from the registry, where
// Windows keeps them, without opening any.
func Devices() ([]Device, error) {
	const root = `SOFTWARE\Microsoft\Windows\CurrentVersion\MMDevices\Audio\Render`
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, root, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	keys, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}
	var out []Device
	for _, n := range keys {
		ep, err := registry.OpenKey(k, n, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		state, _, _ := ep.GetIntegerValue("DeviceState")
		ep.Close()
		if state != 1 { // DEVICE_STATE_ACTIVE
			continue
		}
		props, err := registry.OpenKey(k, n+`\Properties`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		name, _, _ := props.GetStringValue("{a45c254e-df1c-4efd-8020-67d146a850e0},2") // PKEY_Device_DeviceDesc: "Speakers"
		hw, _, _ := props.GetStringValue("{b3f8fa53-0004-438e-9003-51a46e139bfc},6")   // the hardware's name
		inst, _, _ := props.GetStringValue("{b3f8fa53-0004-438e-9003-51a46e139bfc},2") // PKEY_Device_InstanceId
		props.Close()
		if hw != "" {
			name += " (" + hw + ")"
		}
		out = append(out, Device{ID: "{0.0.0.00000000}." + n, Name: name, instance: inst})
	}
	slices.SortFunc(out, func(a, b Device) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Endpoint returns the ID of the active playback device on the USB device
// whose hardware ID contains usb (e.g. "VID_3302&PID_43E6"), or "".
func Endpoint(usb string) (string, error) {
	devs, err := Devices()
	for _, d := range devs {
		if strings.Contains(strings.ToUpper(d.instance), `USB\`+strings.ToUpper(usb)) {
			return d.ID, nil
		}
	}
	return "", err
}

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")

	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = windows.GUID{Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32, Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	// IPolicyConfig is undocumented but stable since Windows 7; Sound settings uses it.
	clsidPolicyConfig = windows.GUID{Data1: 0x870AF99C, Data2: 0x171D, Data3: 0x4F9E, Data4: [8]byte{0xAF, 0x0D, 0xE6, 0x3D, 0xF4, 0x0C, 0x2B, 0xC9}}
	iidIPolicyConfig  = windows.GUID{Data1: 0xF8679F50, Data2: 0x850A, Data3: 0x41CF, Data4: [8]byte{0x9C, 0x72, 0x43, 0x0F, 0x29, 0x02, 0x90, 0xC8}}
)

// Vtable slots.
const (
	clsctxAll = 0x17

	slotRelease           = 2
	slotGetDevice         = 5 // IMMDeviceEnumerator
	slotActivate          = 3 // IMMDevice
	slotIsFormatSupported = 7 // IAudioClient
	slotGetDeviceFormat   = 4 // IPolicyConfig
	slotSetDeviceFormat   = 6
)

// call invokes a COM method by vtable slot and returns its HRESULT.
func call(obj unsafe.Pointer, slot int, args ...uintptr) uint32 {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, slot*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return uint32(r)
}

func release(obj unsafe.Pointer) {
	if obj != nil {
		call(obj, slotRelease)
	}
}

func hrErr(what string, hr uint32) error {
	if hr == 0 {
		return nil
	}
	return fmt.Errorf("%s: HRESULT 0x%08X", what, hr)
}

// withCOM runs fn on a thread with COM set up.
func withCOM(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err {
	case nil, syscall.Errno(1): // S_FALSE: already set up on this thread
		defer windows.CoUninitialize()
	case syscall.Errno(0x80010106): // RPC_E_CHANGED_MODE: set up as single-threaded, which also works
	default:
		return err
	}
	return fn()
}

func create(clsid, iid *windows.GUID) (unsafe.Pointer, error) {
	var obj unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, clsctxAll, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&obj)))
	return obj, hrErr("CoCreateInstance", uint32(hr))
}

// deviceFormat is the endpoint's current format, from IPolicyConfig.
func deviceFormat(policy unsafe.Pointer, id *uint16) (parsed, error) {
	var wfx unsafe.Pointer
	if err := hrErr("GetDeviceFormat", call(policy, slotGetDeviceFormat, uintptr(unsafe.Pointer(id)), 0, uintptr(unsafe.Pointer(&wfx)))); err != nil {
		return parsed{}, err
	}
	defer windows.CoTaskMemFree(wfx)
	size := 18 + int(*(*uint16)(unsafe.Add(wfx, 16))) // WAVEFORMATEX plus cbSize
	return parseWaveFormat(unsafe.Slice((*byte)(wfx), size))
}

// probe reports which formats the endpoint's driver accepts, asking as an
// exclusive-mode stream would.
func probe(id *uint16, channels int, mask uint32) (func(container, valid, rate int) bool, func(), error) {
	enum, err := create(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator)
	if err != nil {
		return nil, nil, err
	}
	defer release(enum)
	var dev, client unsafe.Pointer
	if err := hrErr("GetDevice", call(enum, slotGetDevice, uintptr(unsafe.Pointer(id)), uintptr(unsafe.Pointer(&dev)))); err != nil {
		return nil, nil, err
	}
	defer release(dev)
	if err := hrErr("Activate", call(dev, slotActivate, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client)))); err != nil {
		return nil, nil, err
	}
	ok := func(container, valid, rate int) bool {
		w := waveFormat(channels, rate, container, valid, mask, subtypePCM)
		return call(client, slotIsFormatSupported, 1 /* exclusive */, uintptr(unsafe.Pointer(&w[0])), 0) == 0
	}
	return ok, func() { release(client) }, nil
}

// Formats returns the endpoint's current format and the ones it supports.
func Formats(endpoint string) (current Format, all []Format, err error) {
	id, err := windows.UTF16PtrFromString(endpoint)
	if err != nil {
		return Format{}, nil, err
	}
	err = withCOM(func() error {
		policy, err := create(&clsidPolicyConfig, &iidIPolicyConfig)
		if err != nil {
			return err
		}
		defer release(policy)
		cur, err := deviceFormat(policy, id)
		if err != nil {
			return err
		}
		ok, done, err := probe(id, cur.channels, cur.mask)
		if err != nil {
			return err
		}
		defer done()
		current, all = cur.format, supported(ok)
		return nil
	})
	return current, all, err
}

// SetFormat makes Windows play to the endpoint in f, as Sound settings does:
// the device format in PCM, and the mixer in 32-bit float at the same rate.
func SetFormat(endpoint string, f Format) error {
	id, err := windows.UTF16PtrFromString(endpoint)
	if err != nil {
		return err
	}
	return withCOM(func() error {
		policy, err := create(&clsidPolicyConfig, &iidIPolicyConfig)
		if err != nil {
			return err
		}
		defer release(policy)
		cur, err := deviceFormat(policy, id)
		if err != nil {
			return err
		}
		ok, done, err := probe(id, cur.channels, cur.mask)
		if err != nil {
			return err
		}
		defer done()
		for _, c := range containers(f.Bits) {
			if !slices.Contains(candidateRates, f.Rate) || !ok(c, f.Bits, f.Rate) {
				continue
			}
			dev := waveFormat(cur.channels, f.Rate, c, f.Bits, cur.mask, subtypePCM)
			mix := waveFormat(cur.channels, f.Rate, 32, 32, cur.mask, subtypeFloat)
			return hrErr("SetDeviceFormat", call(policy, slotSetDeviceFormat, uintptr(unsafe.Pointer(id)), uintptr(unsafe.Pointer(&dev[0])), uintptr(unsafe.Pointer(&mix[0]))))
		}
		return errors.New("the device does not support this format")
	})
}

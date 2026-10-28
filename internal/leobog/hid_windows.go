package leobog

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	hid            = windows.NewLazySystemDLL("hid.dll")
	procSetFeature = hid.NewProc("HidD_SetFeature")
	procGetFeature = hid.NewProc("HidD_GetFeature")

	hidGUID = windows.GUID{Data1: 0x4D1E55B2, Data2: 0xF16F, Data3: 0x11CF, Data4: [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30}}
)

// The config collection of a Hi75 on its cable.
// shortcut: wired only; the 2.4 GHz receiver (3554:FA09) is untried.
const configPath = "vid_258a&pid_010c&mi_01&col06"

// PresentPaths lists the config collections of attached keyboards, without opening them.
func PresentPaths() ([]string, error) {
	all, err := windows.CM_Get_Device_Interface_List("", &hidGUID, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, p := range all {
		if strings.Contains(strings.ToLower(p), configPath) {
			out = append(out, p)
		}
	}
	return out, nil
}

// Scan opens the attached keyboard if it answers like a Hi75.
func Scan() ([]*Device, error) {
	paths, err := PresentPaths()
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		h, err := open(p)
		if err != nil {
			continue
		}
		d := &Device{ID: "258A:010C", Name: "LEOBOG Hi75", port: &hidPort{h}, colors: DefaultColors()}
		if err := d.identify(); err != nil {
			windows.CloseHandle(h)
			continue
		}
		return []*Device{d}, nil // shortcut: one keyboard; a second identical one is not shown
	}
	return nil, nil
}

func open(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	// Access 0 is enough for feature reports and works while Windows owns the keyboard.
	return windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
}

// Close releases the keyboard.
func (d *Device) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if p, ok := d.port.(*hidPort); ok && p.h != 0 {
		windows.CloseHandle(p.h)
		p.h = 0
	}
}

type hidPort struct{ h windows.Handle }

func (p *hidPort) set(b []byte) error {
	if r, _, err := procSetFeature.Call(uintptr(p.h), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b))); r == 0 {
		return fmt.Errorf("HidD_SetFeature: %w", err)
	}
	return nil
}

func (p *hidPort) get() ([]byte, error) {
	b := make([]byte, reportLen)
	b[0] = reportID
	if r, _, err := procGetFeature.Call(uintptr(p.h), uintptr(unsafe.Pointer(&b[0])), reportLen); r == 0 {
		return nil, fmt.Errorf("HidD_GetFeature: %w", err)
	}
	return b, nil
}

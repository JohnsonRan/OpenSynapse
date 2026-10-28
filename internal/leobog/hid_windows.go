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
	procAttributes = hid.NewProc("HidD_GetAttributes")

	hidGUID = windows.GUID{Data1: 0x4D1E55B2, Data2: 0xF16F, Data3: 0x11CF, Data4: [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30}}
)

// configPath is how a model's config collection shows in its interface path.
// shortcut: cables only; no model's 2.4 GHz receiver has been tried.
func (m *Model) configPath() string {
	return fmt.Sprintf("vid_%04x&pid_%04x&mi_01&col06", m.VID, m.PID)
}

// modelOf returns the model whose config collection a path is, or nil.
func modelOf(path string) *Model {
	p := strings.ToLower(path)
	for _, m := range Models {
		if strings.Contains(p, m.configPath()) {
			return m
		}
	}
	return nil
}

// PresentPaths lists the config collections of attached keyboards, without opening them.
func PresentPaths() ([]string, error) {
	all, err := windows.CM_Get_Device_Interface_List("", &hidGUID, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, p := range all {
		if modelOf(p) != nil {
			out = append(out, p)
		}
	}
	return out, nil
}

// Scan opens the attached keyboards that answer like their model.
func Scan() ([]*Device, error) {
	paths, err := PresentPaths()
	if err != nil {
		return nil, err
	}
	var out []*Device
	seen := map[*Model]bool{}
	for _, p := range paths {
		m := modelOf(p)
		if seen[m] {
			continue // shortcut: one keyboard per model; a second identical one is not shown
		}
		h, err := open(p)
		if err != nil {
			continue
		}
		d := &Device{Model: m, ID: fmt.Sprintf("%04X:%04X", m.VID, m.PID), Name: m.Name, Firmware: firmware(h), port: &hidPort{h}, colors: DefaultColors()}
		if err := d.identify(); err != nil {
			windows.CloseHandle(h)
			continue
		}
		seen[m] = true
		out = append(out, d)
	}
	return out, nil
}

// firmware is the USB release number as the driver shows it: 0x0500 is 5.0.0.
func firmware(h windows.Handle) string {
	attr := struct {
		Size                uint32
		VendorID, ProductID uint16
		Version             uint16
	}{Size: 10}
	if r, _, _ := procAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&attr))); r == 0 {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", attr.Version>>8, attr.Version>>4&0xf, attr.Version&0xf)
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

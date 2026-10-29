package leobog

import (
	"fmt"
	"slices"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	hid            = windows.NewLazySystemDLL("hid.dll")
	procSetFeature = hid.NewProc("HidD_SetFeature")
	procGetFeature = hid.NewProc("HidD_GetFeature")
	procAttributes = hid.NewProc("HidD_GetAttributes")
	procGetParsed  = hid.NewProc("HidD_GetPreparsedData")
	procFreeParsed = hid.NewProc("HidD_FreePreparsedData")
	procGetCaps    = hid.NewProc("HidP_GetCaps")

	hidGUID = windows.GUID{Data1: 0x4D1E55B2, Data2: 0xF16F, Data3: 0x11CF, Data4: [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30}}
)

// usbIDs reads the vendor and product ID from an interface path.
func usbIDs(path string) (vid, pid uint16, ok bool) {
	p := strings.ToLower(path)
	i := strings.Index(p, "vid_")
	if i < 0 || len(p) < i+17 {
		return 0, 0, false
	}
	_, err := fmt.Sscanf(p[i:i+17], "vid_%04x&pid_%04x", &vid, &pid)
	return vid, pid, err == nil
}

// PresentPaths lists the HID collections of attached keyboards with the USB
// IDs of some model, without opening them.
func PresentPaths() ([]string, error) {
	all, err := windows.CM_Get_Device_Interface_List("", &hidGUID, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, p := range all {
		vid, pid, ok := usbIDs(p)
		if ok && slices.ContainsFunc(Models, func(m *Model) bool { return m.VID == vid && m.PID == pid }) {
			out = append(out, p)
		}
	}
	return out, nil
}

// reportIDs are those the config reports have: 6 on most models, 9 on some.
var reportIDs = []byte{reportID, 0x09}

// Scan opens the attached keyboards whose vendor collection answers with the
// model number of a model.
// shortcut: cables only; no model's 2.4 GHz receiver has been tried.
func Scan() ([]*Device, error) {
	paths, err := PresentPaths()
	if err != nil {
		return nil, err
	}
	var out []*Device
	seen := map[string]bool{}
	for _, p := range paths {
		vid, pid, _ := usbIDs(p)
		id := fmt.Sprintf("%04X:%04X", vid, pid)
		if seen[id] {
			continue // shortcut: one keyboard per USB ID; a second one is not shown
		}
		h, err := open(p)
		if err != nil {
			continue
		}
		if d := probe(h, vid, pid); d != nil {
			d.ID = id
			seen[id] = true
			out = append(out, d)
			continue
		}
		windows.CloseHandle(h)
	}
	return out, nil
}

// probe asks a vendor collection with 520-byte feature reports for its model number.
func probe(h windows.Handle, vid, pid uint16) *Device {
	if usagePage, featureLen := caps(h); usagePage < 0xff00 || featureLen != reportLen {
		return nil
	}
	for _, rid := range reportIDs {
		port := &hidPort{h, rid}
		ident, err := hello(port)
		if err != nil {
			continue
		}
		if m := modelFor(vid, pid, ident); m != nil {
			return &Device{Model: m, Name: m.Name, Firmware: firmware(h), port: port, colors: DefaultColors()}
		}
	}
	return nil
}

func caps(h windows.Handle) (usagePage, featureLen int) {
	var pp uintptr
	if r, _, _ := procGetParsed.Call(uintptr(h), uintptr(unsafe.Pointer(&pp))); r == 0 {
		return 0, 0
	}
	defer procFreeParsed.Call(pp)
	var c struct {
		Usage, UsagePage                                           uint16
		InputReportLength, OutputReportLength, FeatureReportLength uint16
		_                                                          [27]uint16
	}
	procGetCaps.Call(pp, uintptr(unsafe.Pointer(&c)))
	return int(c.UsagePage), int(c.FeatureReportLength)
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

// hidPort sends reports with the keyboard's report ID; the package builds and
// checks them with ID 6.
type hidPort struct {
	h   windows.Handle
	rid byte
}

func (p *hidPort) set(report []byte) error {
	b := append([]byte{p.rid}, report[1:]...)
	if r, _, err := procSetFeature.Call(uintptr(p.h), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b))); r == 0 {
		return fmt.Errorf("HidD_SetFeature: %w", err)
	}
	return nil
}

func (p *hidPort) get() ([]byte, error) {
	b := make([]byte, reportLen)
	b[0] = p.rid
	if r, _, err := procGetFeature.Call(uintptr(p.h), uintptr(unsafe.Pointer(&b[0])), reportLen); r == 0 {
		return nil, fmt.Errorf("HidD_GetFeature: %w", err)
	}
	if b[0] == p.rid {
		b[0] = reportID
	}
	return b, nil
}

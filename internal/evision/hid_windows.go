package evision

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	hid            = windows.NewLazySystemDLL("hid.dll")
	procGetParsed  = hid.NewProc("HidD_GetPreparsedData")
	procFreeParsed = hid.NewProc("HidD_FreePreparsedData")
	procGetCaps    = hid.NewProc("HidP_GetCaps")
	procProduct    = hid.NewProc("HidD_GetProductString")

	hidGUID = windows.GUID{Data1: 0x4D1E55B2, Data2: 0xF16F, Data3: 0x11CF, Data4: [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30}}
)

const usagePage = 0xFF1C

// vendorIDs are Sonix's and EVision's, and those of brands reusing their
// firmware (SPC Gear, VGN); a keyboard counts only once it answers.
var vendorIDs = []uint16{0x0C45, 0x320F, 0x3299, 0x373B}

// v1 are the keyboards OpenRGB drives with the first firmware, which cannot
// be recognized by an answer.
var v1 = [][2]uint16{
	{0x0C45, 0x5004}, {0x0C45, 0x5104}, {0x0C45, 0x5204}, {0x0C45, 0x652F}, {0x0C45, 0x7672}, {0x0C45, 0x7698}, {0x0C45, 0x8520},
	{0x320F, 0x5000}, {0x320F, 0x502A}, {0x320F, 0x505B}, {0x320F, 0x5064}, {0x320F, 0x5078}, {0x320F, 0x5084},
}

func usbIDs(path string) (vid, pid uint16, ok bool) {
	p := strings.ToLower(path)
	i := strings.Index(p, "vid_")
	if i < 0 || len(p) < i+17 {
		return 0, 0, false
	}
	_, err := fmt.Sscanf(p[i:i+17], "vid_%04x&pid_%04x", &vid, &pid)
	return vid, pid, err == nil
}

// PresentPaths lists the HID collections of attached devices with those
// vendor IDs, without opening them.
func PresentPaths() ([]string, error) {
	all, err := windows.CM_Get_Device_Interface_List("", &hidGUID, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, p := range all {
		if vid, _, ok := usbIDs(p); ok && slices.Contains(vendorIDs, vid) {
			out = append(out, p)
		}
	}
	return out, nil
}

// Scan opens the attached keyboards on EVision firmware: V2 ones that answer
// with their capabilities, and the known V1 ones.
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
		if seen[id] || !isConfig(p) {
			continue // shortcut: one keyboard per USB ID; a second one is not shown
		}
		port, err := openPort(p)
		if err != nil {
			continue
		}
		d := &Device{ID: id, port: port}
		// V1 is not asked anything it may not know.
		if !slices.Contains(v1, [2]uint16{vid, pid}) && !d.identify() {
			port.close()
			continue
		}
		if d.Name = productString(port.w); d.Name == "" {
			d.Name = "EVision keyboard " + id
		}
		seen[id] = true
		out = append(out, d)
	}
	return out, nil
}

func isConfig(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var pp uintptr
	if r, _, _ := procGetParsed.Call(uintptr(h), uintptr(unsafe.Pointer(&pp))); r == 0 {
		return false
	}
	defer procFreeParsed.Call(pp)
	var c struct {
		Usage, UsagePage                                           uint16
		InputReportLength, OutputReportLength, FeatureReportLength uint16
		_                                                          [27]uint16
	}
	procGetCaps.Call(pp, uintptr(unsafe.Pointer(&c)))
	return c.UsagePage == usagePage && c.OutputReportLength == reportLen
}

func productString(h windows.Handle) string {
	buf := make([]uint16, 128)
	if r, _, _ := procProduct.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2)); r == 0 {
		return ""
	}
	return strings.TrimSpace(windows.UTF16ToString(buf))
}

// hidPort writes on one handle and reads on another: synchronous handles
// serialize I/O, so a pending read would block every write.
type hidPort struct {
	w, r windows.Handle
	in   chan []byte
	once sync.Once
}

func openPort(path string) (*hidPort, error) {
	p16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE)
	w, err := windows.CreateFile(p16, windows.GENERIC_WRITE, share, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, err
	}
	r, err := windows.CreateFile(p16, windows.GENERIC_READ, share, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		windows.CloseHandle(w)
		return nil, err
	}
	p := &hidPort{w: w, r: r, in: make(chan []byte, 8)}
	go func() {
		defer close(p.in)
		for {
			b := make([]byte, reportLen)
			var n uint32
			if windows.ReadFile(r, b, &n, nil) != nil {
				return // unplugged or closed
			}
			select {
			case p.in <- b[:n]:
			default: // nobody is waiting
			}
		}
	}()
	return p, nil
}

func (p *hidPort) send(report []byte) error {
	var n uint32
	return windows.WriteFile(p.w, report, &n, nil)
}

func (p *hidPort) replies() <-chan []byte { return p.in }

func (p *hidPort) close() {
	p.once.Do(func() {
		windows.CancelIoEx(p.r, nil)
		windows.CloseHandle(p.r)
		windows.CloseHandle(p.w)
	})
}

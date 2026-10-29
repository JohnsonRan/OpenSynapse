package via

import (
	"fmt"
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

	hidGUID = windows.GUID{Data1: 0x4D1E55B2, Data2: 0xF16F, Data3: 0x11CF, Data4: [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30}}
)

// VIA's raw HID collection.
const (
	usagePage = 0xFF60
	usage     = 0x61
)

func usbIDs(path string) (vid, pid uint16, ok bool) {
	p := strings.ToLower(path)
	i := strings.Index(p, "vid_")
	if i < 0 || len(p) < i+17 {
		return 0, 0, false
	}
	_, err := fmt.Sscanf(p[i:i+17], "vid_%04x&pid_%04x", &vid, &pid)
	return vid, pid, err == nil
}

// PresentPaths lists the HID collections of attached keyboards that VIA
// knows, without opening them.
func PresentPaths() ([]string, error) {
	all, err := windows.CM_Get_Device_Interface_List("", &hidGUID, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil, err
	}
	models := Models()
	out := all[:0]
	for _, p := range all {
		if vid, pid, ok := usbIDs(p); ok && models[[2]uint16{vid, pid}] != nil {
			out = append(out, p)
		}
	}
	return out, nil
}

// Scan opens the attached keyboards whose VIA collection answers.
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
		if seen[id] || !isRawHID(p) {
			continue // shortcut: one keyboard per USB ID; a second one is not shown
		}
		port, err := openPort(p)
		if err != nil {
			continue
		}
		d := &Device{Model: Models()[[2]uint16{vid, pid}], ID: id, port: port}
		if err := d.open(); err != nil {
			port.close()
			continue
		}
		seen[id] = true
		out = append(out, d)
	}
	return out, nil
}

func isRawHID(path string) bool {
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
	return c.UsagePage == usagePage && c.Usage == usage && c.OutputReportLength == ReportLen+1
}

// hidPort writes on one handle and reads on another: synchronous handles
// serialize I/O, so a pending read would block every write. Reports go with
// report ID 0 in front, which the package does not see.
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
			b := make([]byte, ReportLen+1)
			var n uint32
			if windows.ReadFile(r, b, &n, nil) != nil {
				return // unplugged or closed
			}
			select {
			case p.in <- b[1:n]:
			default: // nobody is waiting
			}
		}
	}()
	return p, nil
}

func (p *hidPort) send(report []byte) error {
	var n uint32
	return windows.WriteFile(p.w, append([]byte{0}, report...), &n, nil)
}

func (p *hidPort) replies() <-chan []byte { return p.in }

func (p *hidPort) close() {
	p.once.Do(func() {
		windows.CancelIoEx(p.r, nil)
		windows.CloseHandle(p.r)
		windows.CloseHandle(p.w)
	})
}

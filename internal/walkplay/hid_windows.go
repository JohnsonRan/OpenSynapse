package walkplay

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WalkPlay dongles come from many vendors. These are the vendor IDs and the
// product IDs of the firmware family the EPZ TP35 Pro belongs to (SchemeNo16
// in devicePEQ: 10 bands, -10 to 10 dB, shelves); other families differ in
// band count and need their own handling.
var (
	vendorIDs  = []uint16{0x0104, 0x011B, 0x011D, 0x0661, 0x0663, 0x0666, 0x0762, 0x0909, 0x0D8C, 0x2FC6, 0x3302, 0x34BE, 0x35D8, 0x36A7, 0x373B, 0x60C1, 0x60E1, 0xB445, 0xB44D}
	productIDs = []uint16{0x011D, 0x39E0, 0x4301, 0x4302, 0x4304, 0x4305, 0x4306, 0x430D, 0x430E, 0x430F, 0x4311, 0x4312, 0x4313, 0x4316, 0x4318, 0x4319, 0x431D, 0x4321, 0x4322, 0x4323, 0x4324, 0x4325, 0x4326, 0x4327, 0x4329, 0x432A, 0x432C, 0x4351, 0x4352, 0x4355, 0x4358, 0x4359, 0x435A, 0x435C, 0x435D, 0x435E, 0x4360, 0x4361, 0x4363, 0x4364, 0x4366, 0x4367, 0x4380, 0x4381, 0x4382, 0x4383, 0x4386, 0x43B1, 0x43B6, 0x43B7, 0x43B8, 0x43BC, 0x43BE, 0x43BF, 0x43C0, 0x43C2, 0x43C5, 0x43C6, 0x43C7, 0x43C8, 0x43C9, 0x43CA, 0x43CC, 0x43CD, 0x43CF, 0x43D7, 0x43D8, 0x43DA, 0x43DB, 0x43DE, 0x43E1, 0x43E4, 0x43E6, 0x43E8, 0x43EC, 0x43EF, 0x9201, 0x98D4, 0xEE10, 0xEE20, 0xF808}
)

// names are tried dongles; others are named by their USB product string.
var names = map[[2]uint16]string{{0x3302, 0x43E6}: "EPZ TP35 Pro"}

var hidGUID = windows.GUID{Data1: 0x4D1E55B2, Data2: 0xF16F, Data3: 0x11CF, Data4: [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30}}

// usbIDs reads the vendor and product ID from a device path.
func usbIDs(path string) (vid, pid uint16, ok bool) {
	lp := strings.ToLower(path)
	i := strings.Index(lp, "vid_")
	if i < 0 || len(lp) < i+17 {
		return 0, 0, false
	}
	_, err := fmt.Sscanf(lp[i:i+17], "vid_%04x&pid_%04x", &vid, &pid)
	return vid, pid, err == nil
}

func candidate(path string) bool {
	vid, pid, ok := usbIDs(path)
	return ok && slices.Contains(vendorIDs, vid) && slices.Contains(productIDs, pid)
}

// PresentPaths lists the HID interface paths of attached candidate dongles,
// without opening them.
func PresentPaths() ([]string, error) {
	all, err := windows.CM_Get_Device_Interface_List("", &hidGUID, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, p := range all {
		if candidate(p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// Scan opens the attached dongles. A candidate counts only once it answers a
// firmware-version read, so an unrelated device sharing an ID is left alone.
func Scan() ([]*Device, error) {
	paths, err := PresentPaths()
	if err != nil {
		return nil, err
	}
	var out []*Device
	seen := map[[2]uint16]bool{}
	for _, p := range paths {
		vid, pid, _ := usbIDs(p)
		key := [2]uint16{vid, pid}
		if seen[key] {
			continue // shortcut: one dongle per model; two identical ones show as one
		}
		hp, err := openPort(p)
		if err != nil {
			continue
		}
		d := &Device{ID: fmt.Sprintf("%04X:%04X", vid, pid), USB: fmt.Sprintf("VID_%04X&PID_%04X", vid, pid), port: hp}
		if _, err := d.Version(); err != nil {
			hp.close()
			continue
		}
		seen[key] = true
		if d.Name = names[key]; d.Name == "" {
			d.Name = productString(hp.w)
		}
		out = append(out, d)
	}
	return out, nil
}

var procProduct = windows.NewLazySystemDLL("hid.dll").NewProc("HidD_GetProductString")

func productString(h windows.Handle) string {
	buf := make([]uint16, 128)
	if r, _, _ := procProduct.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2)); r == 0 {
		return "WalkPlay USB DAC"
	}
	return strings.TrimSpace(windows.UTF16ToString(buf))
}

// Close releases the dongle.
func (d *Device) Close() {
	if p, ok := d.port.(*hidPort); ok {
		p.close()
	}
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
	p := &hidPort{w: w, r: r, in: make(chan []byte, 32)}
	go func() {
		defer close(p.in)
		for {
			b := make([]byte, 64)
			var n uint32
			if windows.ReadFile(r, b, &n, nil) != nil {
				return // unplugged or closed
			}
			select {
			case p.in <- b[:n]:
			default: // nobody is waiting: volume key reports and late replies
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

package razer

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	hid                   = windows.NewLazySystemDLL("hid.dll")
	procGetPreparsedData  = hid.NewProc("HidD_GetPreparsedData")
	procFreePreparsedData = hid.NewProc("HidD_FreePreparsedData")
	procGetCaps           = hid.NewProc("HidP_GetCaps")
	procSetFeature        = hid.NewProc("HidD_SetFeature")
	procGetFeature        = hid.NewProc("HidD_GetFeature")

	hidGUID = windows.GUID{Data1: 0x4D1E55B2, Data2: 0xF16F, Data3: 0x11CF, Data4: [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30}}
)

type hidpCaps struct {
	Usage, UsagePage                                                   uint16
	InputReportByteLength, OutputReportByteLength, FeatureReportLength uint16
	_                                                                  [17]uint16
	_                                                                  [10]uint16
}

// PresentPaths lists the HID interface paths of attached Razer devices without
// opening them, so polling it to notice plug and unplug is cheap.
func PresentPaths() ([]string, error) {
	all, err := windows.CM_Get_Device_Interface_List("", &hidGUID, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, p := range all {
		if strings.Contains(strings.ToLower(p), "vid_1532&pid_") {
			out = append(out, p)
		}
	}
	return out, nil
}

// hidPaths lists HID collection paths of Razer devices with a 91-byte feature report.
func hidPaths() (map[string]uint16, error) {
	all, err := windows.CM_Get_Device_Interface_List("", &hidGUID, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return nil, err
	}
	out := map[string]uint16{}
	for _, p := range all {
		lp := strings.ToLower(p)
		i := strings.Index(lp, "vid_1532&pid_")
		if i < 0 {
			continue
		}
		var pid uint16
		if _, err := fmt.Sscanf(lp[i+13:i+17], "%04x", &pid); err != nil {
			continue
		}
		if featureLen(p) == ReportLen {
			out[p] = pid
		}
	}
	return out, nil
}

func openPath(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	// Access 0 is enough for feature reports and works while Windows owns the mouse collection.
	return windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
}

func featureLen(path string) int {
	h, err := openPath(path)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(h)
	var pp uintptr
	if r, _, _ := procGetPreparsedData.Call(uintptr(h), uintptr(unsafe.Pointer(&pp))); r == 0 {
		return 0
	}
	defer procFreePreparsedData.Call(pp)
	var caps hidpCaps
	procGetCaps.Call(pp, uintptr(unsafe.Pointer(&caps)))
	return int(caps.FeatureReportLength)
}

// conn is one open feature collection. Requests are serialized: a feature report
// is a set/get pair, so interleaving would read another request's answer.
type conn struct {
	mu   sync.Mutex
	path string
	h    windows.Handle
}

func (c *conn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.h != 0 {
		windows.CloseHandle(c.h)
		c.h = 0
	}
}

// transact sends req and returns the validated response.
func (c *conn) transact(req Report) (Report, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.h == 0 {
		h, err := openPath(c.path)
		if err != nil {
			return Report{}, err
		}
		c.h = h
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if r, _, err := procSetFeature.Call(uintptr(c.h), uintptr(unsafe.Pointer(&req[0])), ReportLen); r == 0 {
			lastErr = fmt.Errorf("HidD_SetFeature: %w", err)
			windows.CloseHandle(c.h) // device may have been unplugged; reopen next time
			c.h = 0
			return Report{}, lastErr
		}
		// Wireless receivers need time to relay the request to the mouse.
		var busy *Report
		for read := 0; read < 10; read++ {
			time.Sleep(time.Duration(2+read*2) * time.Millisecond)
			var resp Report
			if r, _, err := procGetFeature.Call(uintptr(c.h), uintptr(unsafe.Pointer(&resp[0])), ReportLen); r == 0 {
				lastErr = fmt.Errorf("HidD_GetFeature: %w", err)
				break
			}
			err := resp.check(&req)
			if err == nil && resp[offStatus] == statusBusy {
				busy = &resp // still working; keep reading for the final answer
				continue
			}
			if err == nil || err == ErrUnsupported {
				return resp, err
			}
			lastErr = err
			if err == ErrNoAnswer {
				break // resend
			}
		}
		if busy != nil {
			return *busy, nil // openrazer treats busy as accepted
		}
	}
	return Report{}, lastErr
}

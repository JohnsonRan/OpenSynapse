package razer

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procCancelIoEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("CancelIoEx")

// WatchDPI calls fn with the sensitivity a mouse reports when its DPI button
// changes it, until stop is closed. Mice send it as input report 05 02 with X
// and Y big-endian on a collection of usage page 1, usage 0 (seen on the
// DeathAdder V4 Pro, through the receiver). It returns at once; reading runs
// on its own goroutines.
func WatchDPI(pid uint16, stop <-chan struct{}, fn func(DPIStage)) {
	paths, err := PresentPaths()
	if err != nil {
		return
	}
	tag := fmt.Sprintf("pid_%04x", pid)
	for _, p := range paths {
		if !strings.Contains(strings.ToLower(p), tag) {
			continue
		}
		h, ok := openEvents(p)
		if !ok {
			continue
		}
		var once sync.Once
		closeH := func() { once.Do(func() { procCancelIoEx.Call(uintptr(h), 0); windows.CloseHandle(h) }) }
		go func() { <-stop; closeH() }()
		go func() {
			defer closeH()
			buf := make([]byte, 64)
			for {
				var n uint32
				if windows.ReadFile(h, buf, &n, nil) != nil {
					return // unplugged, or stopped
				}
				if n >= 6 && buf[0] == 0x05 && buf[1] == 0x02 {
					fn(DPIStage{X: be16(buf[2:]), Y: be16(buf[4:])})
				}
			}
		}()
	}
}

// openEvents opens a collection that carries device events: usage page 1,
// usage 0, with input reports. Keyboard and mouse collections refuse reading.
func openEvents(path string) (windows.Handle, bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, false
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0, false
	}
	var pp uintptr
	if r, _, _ := procGetPreparsedData.Call(uintptr(h), uintptr(unsafe.Pointer(&pp))); r == 0 {
		windows.CloseHandle(h)
		return 0, false
	}
	var caps hidpCaps
	procGetCaps.Call(pp, uintptr(unsafe.Pointer(&caps)))
	procFreePreparsedData.Call(pp)
	if caps.UsagePage != 1 || caps.Usage != 0 || caps.InputReportByteLength == 0 {
		windows.CloseHandle(h)
		return 0, false
	}
	return h, true
}

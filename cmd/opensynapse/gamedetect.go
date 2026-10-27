package main

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	procGetWindowRect  = user32.NewProc("GetWindowRect")
	procMonitorFromWin = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfo = user32.NewProc("GetMonitorInfoW")
	procGetWindowLong  = user32.NewProc("GetWindowLongPtrW")
	procGetClassName   = user32.NewProc("GetClassNameW")
	pdh                = windows.NewLazySystemDLL("pdh.dll")
	procPdhOpenQuery   = pdh.NewProc("PdhOpenQueryW")
	procPdhAddCounter  = pdh.NewProc("PdhAddEnglishCounterW")
	procPdhCollect     = pdh.NewProc("PdhCollectQueryData")
	procPdhGetArray    = pdh.NewProc("PdhGetFormattedCounterArrayW")
	procPdhCloseQuery  = pdh.NewProc("PdhCloseQuery")
)

type rect struct{ L, T, R, B int32 }

// foreground describes the foreground window.
type foreground struct {
	pid        uint32
	exe        string
	fullscreen bool // covers its whole monitor without a caption: exclusive or borderless
}

func getForeground() foreground {
	hwnd := windows.GetForegroundWindow()
	if hwnd == 0 {
		return foreground{}
	}
	var f foreground
	if _, err := windows.GetWindowThreadProcessId(hwnd, &f.pid); err != nil || f.pid == 0 {
		return foreground{}
	}
	f.exe = exeName(f.pid)
	f.fullscreen = isFullscreen(hwnd)
	return f
}

func exeName(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	p := windows.UTF16ToString(buf[:n])
	return strings.ToLower(p[strings.LastIndexAny(p, `\/`)+1:])
}

func isFullscreen(hwnd windows.HWND) bool {
	// The desktop and taskbar also cover the screen.
	cls := make([]uint16, 64)
	procGetClassName.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&cls[0])), uintptr(len(cls)))
	switch windows.UTF16ToString(cls) {
	case "Progman", "WorkerW", "Shell_TrayWnd", "Shell_SecondaryTrayWnd":
		return false
	}
	const gwlStyle, wsCaption = ^uintptr(15), 0x00C00000 // GWL_STYLE = -16
	if style, _, _ := procGetWindowLong.Call(uintptr(hwnd), gwlStyle); style&wsCaption == wsCaption {
		return false // a maximized normal window, not a borderless game
	}
	var w rect
	if r, _, _ := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&w))); r == 0 {
		return false
	}
	mon, _, _ := procMonitorFromWin.Call(uintptr(hwnd), 2) // MONITOR_DEFAULTTONEAREST
	mi := struct {
		Size          uint32
		Monitor, Work rect
		Flags         uint32
	}{Size: 40}
	if r, _, _ := procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return false
	}
	m := mi.Monitor
	return w.L <= m.L && w.T <= m.T && w.R >= m.R && w.B >= m.B
}

// gpuMeter reads per-process 3D-engine GPU load. It is opened only while a
// fullscreen window is in front, so it costs nothing on the desktop.
type gpuMeter struct {
	query, counter uintptr
	primed         bool
}

func (g *gpuMeter) close() {
	if g.query != 0 {
		procPdhCloseQuery.Call(g.query)
	}
	*g = gpuMeter{}
}

// usage returns the summed 3D-engine utilization of pid in percent, or -1
// until two samples exist (PDH rates need a previous sample).
func (g *gpuMeter) usage(pid uint32) float64 {
	if g.query == 0 {
		if r, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&g.query))); r != 0 {
			return -1
		}
		path, _ := windows.UTF16PtrFromString(`\GPU Engine(*)\Utilization Percentage`)
		if r, _, _ := procPdhAddCounter.Call(g.query, uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&g.counter))); r != 0 {
			g.close()
			return -1
		}
	}
	procPdhCollect.Call(g.query)
	if !g.primed {
		g.primed = true
		return -1
	}
	const fmtDouble, moreData = 0x00000200 | 0x00008000, 0x800007D2 // PDH_FMT_DOUBLE|NOCAP100
	var size, count uint32
	if r, _, _ := procPdhGetArray.Call(g.counter, fmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0); r != moreData || size == 0 {
		return -1
	}
	buf := make([]byte, size)
	if r, _, _ := procPdhGetArray.Call(g.counter, fmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0]))); r != 0 {
		return -1
	}
	type item struct {
		name   *uint16
		status uint32
		_      uint32
		value  float64
	}
	items := unsafe.Slice((*item)(unsafe.Pointer(&buf[0])), count)
	prefix := fmt.Sprintf("pid_%d_", pid)
	var sum float64
	for _, it := range items {
		if it.status > 1 { // PDH_CSTATUS_VALID_DATA / NEW_DATA
			continue
		}
		n := windows.UTF16PtrToString(it.name)
		if strings.HasPrefix(n, prefix) && strings.HasSuffix(n, "engtype_3D") {
			sum += it.value
		}
	}
	return sum
}

package main

import (
	"log"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Low-level input: the keyboard and mouse hooks that catch trigger keys and
// record macros, and SendInput for playback.

var (
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessage          = user32.NewProc("GetMessageW")
	procPeekMessage         = user32.NewProc("PeekMessageW")
	procPostThreadMessage   = user32.NewProc("PostThreadMessageW")
	procSendInput           = user32.NewProc("SendInput")
	procMapVirtualKey       = user32.NewProc("MapVirtualKeyW")
)

const (
	whKeyboardLL  = 13
	whMouseLL     = 14
	wmApp         = 0x8000
	wmKeyDown     = 0x0100
	wmSysKeyDown  = 0x0104
	llkhfInjected = 0x10
	llmhfInjected = 0x01
)

type kbdHookData struct {
	vk, scan, flags, time uint32
	extra                 uintptr
}

type mouseHookData struct {
	x, y              int32
	data, flags, time uint32
	extra             uintptr
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wparam  uintptr
	lparam  uintptr
	time    uint32
	x, y    int32
	private uint32
}

var hookThread struct {
	sync.Mutex
	id uint32
}

// runHooks owns the hooks on one locked thread with its own message loop.
// Every input event on the PC passes through an installed hook, so the
// keyboard hook is installed only while a button is bound to a macro or one is
// being recorded, and the mouse hook only while recording.
func runHooks() {
	runtime.LockOSThread()
	var msg winMsg
	procPeekMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 0) // creates the queue PostThreadMessage needs
	hookThread.Lock()
	hookThread.id = windows.GetCurrentThreadId()
	hookThread.Unlock()
	var mod windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &mod)
	kbCB, msCB := windows.NewCallback(keyboardProc), windows.NewCallback(mouseProc)
	var kb, ms uintptr
	set := func(h uintptr, want bool, kind int, cb uintptr) uintptr {
		switch {
		case want && h == 0:
			h, _, err := procSetWindowsHookEx.Call(uintptr(kind), cb, uintptr(mod), 0)
			if h == 0 {
				log.Println("input hook:", err)
			}
			return h
		case !want && h != 0:
			procUnhookWindowsHookEx.Call(h)
			return 0
		}
		return h
	}
	for {
		wantKB, wantMouse := hooksWanted()
		kb = set(kb, wantKB, whKeyboardLL, kbCB)
		ms = set(ms, wantMouse, whMouseLL, msCB)
		// Wait for refreshHooks; hook callbacks run inside GetMessage.
		for {
			r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			if msg.message == wmApp {
				break
			}
		}
	}
}

// refreshHooks makes the hook thread install or remove hooks to match hooksWanted.
func refreshHooks() {
	hookThread.Lock()
	id := hookThread.id
	hookThread.Unlock()
	if id != 0 {
		procPostThreadMessage.Call(uintptr(id), wmApp, 0, 0)
	}
}

func keyboardProc(code, wparam, lparam uintptr) uintptr {
	if int32(code) == 0 {
		k := (*kbdHookData)(unsafe.Pointer(lparam))
		if k.flags&llkhfInjected == 0 { // never react to our own playback
			down := wparam == wmKeyDown || wparam == wmSysKeyDown
			if onTriggerKey(int(k.vk), down) {
				return 1 // swallowed: the trigger key never reaches other programs
			}
			recordInput(MacroEvent{Kind: "key", Code: int(k.vk), Down: down}, k.time)
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, code, wparam, lparam)
	return r
}

func mouseProc(code, wparam, lparam uintptr) uintptr {
	if int32(code) == 0 {
		m := (*mouseHookData)(unsafe.Pointer(lparam))
		if btn, down, ok := mouseButton(uint32(wparam), m.data); ok && m.flags&llmhfInjected == 0 {
			recordInput(MacroEvent{Kind: "mouse", Code: btn, Down: down}, m.time)
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, code, wparam, lparam)
	return r
}

// mouseButton decodes a button message: 1 left, 2 right, 3 middle, 4 back, 5 forward.
func mouseButton(msg, data uint32) (btn int, down, ok bool) {
	switch msg {
	case 0x0201, 0x0202:
		return 1, msg == 0x0201, true
	case 0x0204, 0x0205:
		return 2, msg == 0x0204, true
	case 0x0207, 0x0208:
		return 3, msg == 0x0207, true
	case 0x020B, 0x020C:
		return 3 + int(data>>16), msg == 0x020B, data>>16 == 1 || data>>16 == 2
	}
	return 0, false, false
}

// INPUT for SendInput, as its keyboard and mouse variants (40 bytes on amd64).
type keyInput struct {
	typ, _      uint32
	vk, scan    uint16
	flags, time uint32
	extra       uintptr
	_           [8]byte
}

type mouseInput struct {
	typ, _            uint32
	dx, dy            int32
	data, flags, time uint32
	extra             uintptr
}

// sendEvent is replaced in tests.
var sendEvent = sendInput

func sendInput(e MacroEvent) {
	if e.Kind == "mouse" {
		in := mouseInput{}
		bits := [...]uint32{1: 0x0002, 2: 0x0008, 3: 0x0020, 4: 0x0080, 5: 0x0080}
		if e.Code < 1 || e.Code > 5 {
			return
		}
		in.flags = bits[e.Code]
		if !e.Down {
			in.flags <<= 1 // each *UP flag follows its *DOWN flag
		}
		if e.Code >= 4 {
			in.data = uint32(e.Code - 3) // XBUTTON1 / XBUTTON2
		}
		procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
		return
	}
	// Scan codes, not virtual keys: games that read raw input only see these.
	in := keyInput{typ: 1, vk: uint16(e.Code)}
	if sc, _, _ := procMapVirtualKey.Call(uintptr(e.Code), 4); sc != 0 { // MAPVK_VK_TO_VSC_EX
		in.scan, in.flags = uint16(sc&0xFF), 0x0008 // KEYEVENTF_SCANCODE
		if sc>>8 == 0xE0 || sc>>8 == 0xE1 {
			in.flags |= 0x0001 // KEYEVENTF_EXTENDEDKEY
		}
	}
	if !e.Down {
		in.flags |= 0x0002 // KEYEVENTF_KEYUP
	}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

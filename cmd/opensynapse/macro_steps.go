package main

import (
	"errors"
	"log"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

// Two macro steps do more than press a key: a text step types its text, and a
// launch step opens a program, a file or a link. A backup file can carry
// both, so the rules here hold for a step whoever wrote it:
//   - the settings file is trusted, a backup file is not: a macro that comes
//     from a backup with a launch step does not play until the user allows it
//     on this PC (Macro.Imported, which only Go sets and clears);
//   - a launch target is a full path or a link, never a name Windows would
//     search for, and never a path or link that makes this process contact a
//     server;
//   - a macro with a text or launch step that breaks a rule does not play at
//     all: its other steps would land in the wrong window.

const (
	maxTextUnits   = 1000 // UTF-16 code units, as the page's maxlength counts them
	maxTargetRunes = 2000
)

// textPace is the wait after each typed character, in ms. Programs lose
// characters sent in one burst. Tests shorten it.
var textPace = 5

var (
	drivePath  = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
	linkScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]+:`) // two characters or more: one is a drive
)

// cleanStep returns a text or launch step as it is stored, or the message key
// of the rule it breaks. It only reads the strings: checking a step from a
// backup file must not touch a path named in it.
func cleanStep(e MacroEvent) (MacroEvent, string) {
	// An older release sends a step of a kind it does not know as a key event
	// with Code. Zero is no key, so Code, Down, X and Y stay zero in the file.
	out := MacroEvent{Kind: e.Kind, Delay: e.Delay}
	switch e.Kind {
	case "text":
		out.Text = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(e.Text)
		bad := func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }
		if out.Text == "" || len(utf16.Encode([]rune(out.Text))) > maxTextUnits || strings.ContainsFunc(out.Text, bad) {
			return out, "err.macroText"
		}
	case "launch":
		out.Target, out.Args = strings.TrimSpace(e.Target), e.Args
		// A format character (a bidi override, a zero-width space) can make a
		// target read as another one where the user reviews it.
		hidden := func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }
		for _, s := range []string{out.Target, out.Args} {
			if len([]rune(s)) > maxTargetRunes || strings.ContainsFunc(s, hidden) {
				return out, "err.macroLaunch"
			}
		}
		switch {
		case drivePath.MatchString(out.Target):
		case linkScheme.MatchString(out.Target) && !strings.HasPrefix(strings.ToLower(out.Target), "file:"):
			out.Args = ""
		default: // a bare name, a UNC or device path, a file: link
			return out, "err.macroLaunch"
		}
	}
	return out, ""
}

// launches reports whether the macro has a launch step.
func (m Macro) launches() bool {
	return slices.ContainsFunc(m.Events, func(e MacroEvent) bool { return e.Kind == "launch" })
}

// playable returns the steps to play, or the message key that says why the
// macro must not play. The settings file is not checked when it loads, so
// playback checks the text and launch steps again.
func playable(m Macro) ([]MacroEvent, string) {
	launches := m.launches()
	if launches && m.Imported {
		return nil, "macro.blocked"
	}
	if launches && m.Mode != "once" { // a held button would start a program every pass
		return nil, "macro.invalid"
	}
	events := slices.Clone(m.Events)
	for i, e := range events {
		if e.Kind != "text" && e.Kind != "launch" {
			continue
		}
		clean, bad := cleanStep(e)
		if bad != "" {
			return nil, "macro.invalid"
		}
		events[i] = clean
	}
	return events, ""
}

// typeText types a text step. A line break and a tab are pressed as the Enter
// and Tab keys, because many programs act on the key, not on the character.
// Every press is followed at once by its release, so stopping leaves nothing
// held. It reports false when wait says the macro was stopped.
func typeText(text string, wait func(ms int) bool) bool {
	tap := func(vk int) {
		sendEvent(MacroEvent{Kind: "key", Code: vk, Down: true})
		sendEvent(MacroEvent{Kind: "key", Code: vk})
	}
	for _, r := range text {
		switch r {
		case '\n':
			tap(0x0D) // VK_RETURN
		case '\t':
			tap(0x09) // VK_TAB
		default:
			sendChar(r)
		}
		if !wait(textPace) {
			return false
		}
	}
	return true
}

// unicodeInputs is a character as SendInput takes it: a press and a release
// per UTF-16 unit, which Windows delivers without the keyboard layout.
func unicodeInputs(r rune) []keyInput {
	const keyUp, unicodeChar = 0x0002, 0x0004 // KEYEVENTF_KEYUP, KEYEVENTF_UNICODE
	var in []keyInput
	for _, u := range utf16.Encode([]rune{r}) {
		in = append(in, keyInput{typ: 1, scan: u, flags: unicodeChar}, keyInput{typ: 1, scan: u, flags: unicodeChar | keyUp})
	}
	return in
}

// sendUnicode types one character. A surrogate pair goes in one call, so no
// other input lands inside it.
func sendUnicode(r rune) {
	in := unicodeInputs(r)
	procSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
}

// shellExecuteInfo is SHELLEXECUTEINFOW (112 bytes on amd64).
type shellExecuteInfo struct {
	size, mask              uint32
	hwnd                    uintptr
	verb, file, params, dir *uint16
	show                    int32
	instApp, idList         uintptr
	class                   *uint16
	keyClass                uintptr
	hotKey                  uint32
	icon, process           uintptr
}

var procShellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// shellOpen opens target as a double-click would: the default verb, never
// "runas". Target and args reach Windows as two strings; no command line is
// built here and nothing in them is expanded. The process is not waited for.
func shellOpen(target, args, dir string) error {
	const (
		noAsync = 0x0100 // SEE_MASK_NOASYNC: the calling thread ends right after the call
		noUI    = 0x0400 // SEE_MASK_FLAG_NO_UI: a failure is ours to report, not an error box
	)
	info := shellExecuteInfo{mask: noAsync | noUI, show: 1} // SW_SHOWNORMAL
	info.size = uint32(unsafe.Sizeof(info))
	for _, s := range []struct {
		to   **uint16
		from string
	}{{&info.file, target}, {&info.params, args}, {&info.dir, dir}} {
		if s.from == "" {
			continue
		}
		p, err := windows.UTF16PtrFromString(s.from)
		if err != nil {
			return err
		}
		*s.to = p
	}
	// ShellExecuteEx may hand the work to a shell extension, which needs COM
	// on a thread that keeps its apartment for the whole call.
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err == nil {
			defer windows.CoUninitialize()
		}
		if ok, _, err := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&info))); ok == 0 {
			done <- err
			return
		}
		done <- nil
	}()
	return <-done
}

// Tests replace these: no test types into a window, starts a program or
// shows a notification.
var (
	sendChar    = sendUnicode
	startTarget = shellOpen
	notifyMacro = showMacroNotice
)

// showMacroNotice tells the user why a macro did not play. A notification
// also shows with the window closed. It names the macro only: a step's text,
// target and arguments stay out of notifications and the log.
func showMacroNotice(m Macro, bodyKey string) {
	n := mygo.NewNotification(mygo.NotificationOptions{
		ID:    "macro-" + m.ID,
		Title: tr("macro.title", m.Name),
		Body:  tr(bodyKey),
	})
	if err := n.Show(); err != nil {
		log.Println("notify:", err)
	}
}

// runLaunch opens a launch step's target and reports whether the macro goes
// on. After a failure the following steps would land in whatever window is in
// front, so the macro ends.
func runLaunch(m Macro, e MacroEvent) bool {
	dir := ""
	if drivePath.MatchString(e.Target) {
		dir = filepath.Dir(e.Target) // as a double-click in Explorer starts it
	}
	err := startTarget(e.Target, e.Args, dir)
	if err == nil {
		return true
	}
	if errors.Is(err, windows.ERROR_CANCELLED) {
		return false // the user said no to the Windows prompt: nothing to explain
	}
	var code windows.Errno
	errors.As(err, &code)
	log.Printf("macro %s: launch step failed, Windows error %d", m.Name, uint32(code))
	notifyMacro(m, "macro.failed")
	return false
}

// PickTarget asks for the program or file of a launch step. It returns an
// empty path when the user cancels.
func (Macros) PickTarget() (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: currentWindow(), Filters: []mygo.FileFilter{
		{Name: tr("macro.programs"), Extensions: []string{"exe", "lnk"}},
		{Name: tr("macro.allFiles"), Extensions: []string{"*"}},
	}})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// Allow lets a macro from a backup file open its programs and links, once
// the user has seen them.
func (Macros) Allow(id string) (Macro, error) {
	cfgMu.Lock()
	i := slices.IndexFunc(cfg.Macros, func(m Macro) bool { return m.ID == id })
	if i < 0 {
		cfgMu.Unlock()
		return Macro{}, errors.New(tr("err.macroMissing"))
	}
	cfg.Macros[i].Imported = false
	m := cfg.Macros[i]
	cfgMu.Unlock()
	return m, saveConfig()
}

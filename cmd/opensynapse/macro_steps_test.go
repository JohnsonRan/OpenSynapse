package main

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func text(s string) MacroEvent { return MacroEvent{Kind: "text", Text: s} }
func launch(target, args string) MacroEvent {
	return MacroEvent{Kind: "launch", Target: target, Args: args}
}

func TestUnicodeInputs(t *testing.T) {
	if unsafe.Sizeof(shellExecuteInfo{}) != 112 {
		t.Fatal("SHELLEXECUTEINFOW must be 112 bytes on amd64")
	}
	units := func(r rune) (scans []uint16, flags []uint32) {
		for _, in := range unicodeInputs(r) {
			if in.typ != 1 || in.vk != 0 {
				t.Fatalf("%q: input %+v is not a Unicode keyboard input", r, in)
			}
			scans, flags = append(scans, in.scan), append(flags, in.flags)
		}
		return scans, flags
	}
	if s, f := units('a'); !slices.Equal(s, []uint16{0x61, 0x61}) || !slices.Equal(f, []uint32{4, 6}) {
		t.Fatalf("a: %x %x", s, f)
	}
	// A surrogate pair: each half goes down and up, the high half first.
	if s, f := units(0x1F600); !slices.Equal(s, []uint16{0xD83D, 0xD83D, 0xDE00, 0xDE00}) || !slices.Equal(f, []uint32{4, 6, 4, 6}) {
		t.Fatalf("U+1F600: %x %x", s, f)
	}
}

// seams records what a macro would type, start and tell the user, in order.
type seams struct {
	sync.Mutex
	log     []string
	started [][3]string
	notices []string
	fail    error
	stop    chan struct{} // closed at the second start: a looping macro must not hang the test
}

func (s *seams) add(entry string)  { s.Lock(); s.log = append(s.log, entry); s.Unlock() }
func (s *seams) entries() []string { s.Lock(); defer s.Unlock(); return slices.Clone(s.log) }

func recordSteps(t *testing.T) *seams {
	s := &seams{}
	oldEvent, oldChar, oldStart, oldNotify, oldPace := sendEvent, sendChar, startTarget, notifyMacro, textPace
	textPace = 0
	sendEvent = func(e MacroEvent) {
		dir := "up"
		if e.Down {
			dir = "down"
		}
		s.add(e.Kind + " " + string(rune('0'+e.Code/16)) + string("0123456789ABCDEF"[e.Code%16]) + " " + dir)
	}
	sendChar = func(r rune) { s.add("char " + string(r)) }
	startTarget = func(target, args, dir string) error {
		s.add("start")
		s.started = append(s.started, [3]string{target, args, dir})
		if len(s.started) == 2 && s.stop != nil {
			close(s.stop)
		}
		return s.fail
	}
	notifyMacro = func(m Macro, bodyKey string) { s.notices = append(s.notices, bodyKey) }
	t.Cleanup(func() {
		sendEvent, sendChar, startTarget, notifyMacro, textPace = oldEvent, oldChar, oldStart, oldNotify, oldPace
	})
	return s
}

func TestPlayText(t *testing.T) {
	s := recordSteps(t)
	play(Macro{Mode: "once", Events: []MacroEvent{key(0x10, true, 0), text("a\r\nb\t\U0001F600"), key(0x10, false, 0)}}, make(chan struct{}))
	want := []string{"key 10 down", "char a", "key 0D down", "key 0D up", "char b", "key 09 down", "key 09 up", "char \U0001F600", "key 10 up"}
	if got := s.entries(); !slices.Equal(got, want) {
		t.Fatalf("played %q, want %q", got, want)
	}
	// A text step emptied by an older release: none of the macro plays.
	s.log = nil
	play(Macro{Mode: "once", Events: []MacroEvent{key(0x41, true, 0), text(""), key(0x41, false, 0)}}, make(chan struct{}))
	if len(s.entries()) != 0 || !slices.Equal(s.notices, []string{"macro.invalid"}) {
		t.Fatalf("an empty text step: played %q, notices %q", s.entries(), s.notices)
	}
}

// Stopping in the middle of a text leaves no key down: Enter goes down and
// up without a wait in between.
func TestPlayTextStops(t *testing.T) {
	s := recordSteps(t)
	textPace = 1
	const long = 500
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		play(Macro{Mode: "hold", Events: []MacroEvent{text(strings.Repeat("\n", long))}}, stop)
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("play did not stop")
	}
	got := s.entries()
	if len(got) == 0 || len(got) >= 2*long {
		t.Fatalf("sent %d events of %d", len(got), 2*long)
	}
	if len(got)%2 != 0 || got[len(got)-1] != "key 0D up" {
		t.Fatalf("stopped with Enter down: last of %d events is %q", len(got), got[len(got)-1])
	}
	// Stopped at the first wait: a press already sent is released first.
	for txt, want := range map[string][]string{"\n\n": {"key 0D down", "key 0D up"}, "\t\t": {"key 09 down", "key 09 up"}, "ab": {"char a"}} {
		s.log = nil
		waits := 0
		if typeText(txt, func(int) bool { waits++; return false }) || waits != 1 || !slices.Equal(s.entries(), want) {
			t.Fatalf("%q stopped after %d waits with %q", txt, waits, s.entries())
		}
	}
}

func TestPlayLaunch(t *testing.T) {
	s := recordSteps(t)
	run := func(m Macro) {
		s.log, s.started, s.notices, s.stop = nil, nil, nil, make(chan struct{})
		play(m, s.stop)
	}
	steps := func(l MacroEvent) []MacroEvent {
		return []MacroEvent{key(0x41, true, 0), key(0x41, false, 0), l, key(0x42, true, 0), key(0x42, false, 0)}
	}
	all := []string{"key 41 down", "key 41 up", "start", "key 42 down", "key 42 up"}

	run(Macro{Mode: "once", Events: steps(launch(`C:\Tools\app.exe`, "--profile game"))})
	if !slices.Equal(s.entries(), all) || !slices.Equal(s.started, [][3]string{{`C:\Tools\app.exe`, "--profile game", `C:\Tools`}}) || len(s.notices) != 0 {
		t.Fatalf("a program: played %q, started %q, notices %q", s.entries(), s.started, s.notices)
	}
	run(Macro{Mode: "once", Events: steps(launch("steam://run/1", "ignored"))})
	if !slices.Equal(s.started, [][3]string{{"steam://run/1", "", ""}}) {
		t.Fatalf("a link: started %q", s.started)
	}

	// From a backup file and not allowed yet: nothing plays, not even the keys
	// before the launch step.
	run(Macro{Mode: "once", Imported: true, Events: steps(launch(`C:\Tools\app.exe`, ""))})
	if len(s.entries()) != 0 || !slices.Equal(s.notices, []string{"macro.blocked"}) {
		t.Fatalf("an imported macro: played %q, notices %q", s.entries(), s.notices)
	}

	// A settings file is not checked when it loads: a target Save would refuse
	// and a looping mode are refused here.
	for _, m := range []Macro{
		{Mode: "once", Events: steps(launch("notepad", ""))},
		{Mode: "once", Events: steps(launch(`\\host\share\a.exe`, ""))},
		{Mode: "once", Events: steps(launch("FILE:///C:/a.exe", ""))},
		{Mode: "once", Events: steps(launch("", ""))},
		{Mode: "hold", Events: steps(launch(`C:\Tools\app.exe`, ""))},
		{Mode: "repeat", Count: 3, Events: steps(launch(`C:\Tools\app.exe`, ""))},
	} {
		run(m)
		if len(s.entries()) != 0 || !slices.Equal(s.notices, []string{"macro.invalid"}) {
			t.Errorf("%s %+v: played %q, notices %q", m.Mode, m.Events[2], s.entries(), s.notices)
		}
	}

	// The target did not open: the keys after it would land in the wrong window.
	s.fail = windows.ERROR_FILE_NOT_FOUND
	run(Macro{Mode: "once", Events: steps(launch(`C:\Tools\app.exe`, ""))})
	if !slices.Equal(s.entries(), all[:3]) || !slices.Equal(s.notices, []string{"macro.failed"}) {
		t.Fatalf("a failed launch: played %q, notices %q", s.entries(), s.notices)
	}
	// The user declined the Windows prompt: the macro ends without a notice.
	s.fail = windows.ERROR_CANCELLED
	run(Macro{Mode: "once", Events: steps(launch(`C:\Tools\app.exe`, ""))})
	if !slices.Equal(s.entries(), all[:3]) || len(s.notices) != 0 {
		t.Fatalf("a declined launch: played %q, notices %q", s.entries(), s.notices)
	}
}

func TestMacroNormalizeSteps(t *testing.T) {
	long := strings.Repeat("a", maxTextUnits)
	m := Macro{Name: "m", Mode: "once", Events: []MacroEvent{
		{Kind: "text", Text: "a\r\nb\rc", Code: 0x41, Down: true, X: 1, Y: 2, Target: "t", Args: "x", Delay: 7},
		{Kind: "launch", Target: `  C:\Tools\app.exe `, Args: "--x 1", Code: 0x41, Down: true, X: 1, Y: 2, Text: "t"},
		{Kind: "launch", Target: "steam://run/1", Args: "dropped"},
		{Kind: "key", Code: 0x41, Down: true, Text: "t", Target: "t", Args: "x"},
		text(long),
		text("\U0001F468\u200D\U0001F469"), // an emoji sequence needs its zero-width joiner
		launch("C:/Tools/app.exe", ""),
		launch(`shell:AppsFolder\App`, ""),
	}}
	want := []MacroEvent{
		{Kind: "text", Text: "a\nb\nc", Delay: 7},
		{Kind: "launch", Target: `C:\Tools\app.exe`, Args: "--x 1"},
		{Kind: "launch", Target: "steam://run/1"},
		{Kind: "key", Code: 0x41, Down: true},
		text(long),
		text("\U0001F468\u200D\U0001F469"),
		launch("C:/Tools/app.exe", ""),
		launch(`shell:AppsFolder\App`, ""),
	}
	if err := m.normalize(); err != nil || !slices.Equal(m.Events, want) {
		t.Fatalf("%v\n got %+v\nwant %+v", err, m.Events, want)
	}
	for _, bad := range []MacroEvent{
		text(""),
		text(long + "a"),
		text(strings.Repeat("\U0001F600", maxTextUnits/2+1)), // counted in UTF-16 units, as the page does
		text("a\ab"),
		{Kind: "text", Text: "a", Delay: -1},
		launch("", ""),
		launch("notepad", ""),
		launch(`Tools\app.exe`, ""),
		launch(`C:app.exe`, ""), // relative to the drive's current folder, and not a link with the scheme "C"
		launch(`\\host\share\a.exe`, ""),
		launch(`\\?\C:\a.exe`, ""),
		launch("//host/share/a.exe", ""),
		launch("file:///C:/a.exe", ""),
		launch("FILE://host/a.exe", ""),
		launch(`C:\a.exe`, "a\nb"),
		launch("C:\\a\u202Egpj.exe", ""), // a right-to-left override hides the real ending
		launch(`C:\a.exe`, "a\u200Bb"),
		launch(`C:\`+strings.Repeat("a", maxTargetRunes), ""),
	} {
		if (&Macro{Name: "m", Mode: "once", Events: []MacroEvent{bad}}).normalize() == nil {
			t.Errorf("accepted %.60q %.60q %q", bad.Text, bad.Target, bad.Args)
		}
	}
	for _, mode := range []string{"repeat", "toggle", "hold"} {
		if (&Macro{Name: "m", Mode: mode, Count: 2, Events: []MacroEvent{launch(`C:\a.exe`, "")}}).normalize() == nil {
			t.Errorf("accepted a launch step in a macro that plays in mode %s", mode)
		}
	}
	// The page and the bindings use the ID as it is.
	for id, ok := range map[string]bool{"": true, "18a3f09c2b7d4e10": true, "m": false, "18A3": false, `"><img src=x>`: false, "18a3f09c2b7d4e10f": false} {
		if err := (&Macro{ID: id, Name: "m"}).normalize(); (err == nil) != ok {
			t.Errorf("ID %q: %v", id, err)
		}
	}
}

// Whether an imported macro may open programs is Go's to keep: saving from
// the page does not change it, and Allow does.
func TestSaveKeepsImportedUntilAllowed(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	cfgMu.Lock()
	saved := cfg
	cfg = defaultConfig()
	cfg.Macros = []Macro{{ID: "1a", Name: "m", Mode: "once", Imported: true, Events: []MacroEvent{launch(`C:\a.exe`, "")}}}
	cfgMu.Unlock()
	t.Cleanup(func() { cfgMu.Lock(); cfg = saved; cfgMu.Unlock() })
	stored := func() Macro { cfgMu.Lock(); defer cfgMu.Unlock(); return cfg.Macros[0] }

	edited := Macro{ID: "1a", Name: "m", Mode: "once", Events: []MacroEvent{launch(`C:\b.exe`, "")}} // the page says "not imported"
	got, err := Macros{}.Save(edited)
	if err != nil || !got.Imported || !stored().Imported || stored().Events[0].Target != `C:\b.exe` {
		t.Fatalf("saving allowed an imported macro: %v %+v", err, stored())
	}
	fresh, err := Macros{}.Save(Macro{Name: "new", Mode: "once", Imported: true, Events: []MacroEvent{launch(`C:\a.exe`, "")}})
	if err != nil || fresh.Imported {
		t.Fatalf("a macro made on this PC is blocked: %v %+v", err, fresh)
	}
	if _, err := (Macros{}).Allow("ffff", stored().Events); err == nil {
		t.Fatal("allowed a macro that does not exist")
	}
	// The page shows an edit Go refused, or one that has not arrived: what
	// the user reviewed is not what would run.
	for _, shown := range [][]MacroEvent{{launch("notepad", "")}, {launch(`C:\a.exe`, "")}, {launch(`C:\b.exe`, "--x")}, nil} {
		if _, err := (Macros{}).Allow("1a", shown); err == nil || !stored().Imported {
			t.Fatalf("allowed %+v while %+v is stored", shown, stored().Events)
		}
	}
	if got, err := (Macros{}).Allow("1a", []MacroEvent{launch(` C:\b.exe `, "")}); err != nil || got.Imported || stored().Imported {
		t.Fatalf("Allow: %v %+v", err, stored())
	}
	s := recordSteps(t)
	play(stored(), make(chan struct{}))
	if len(s.started) != 1 {
		t.Fatalf("the allowed macro started %q", s.started)
	}
}

// A backup file is not trusted: its launch steps wait for the user, whatever
// the file says about them.
func TestImportConfigBlocksLaunchSteps(t *testing.T) {
	src := defaultConfig()
	src.Macros = []Macro{
		{ID: "1a", Name: "opens", Mode: "once", Events: []MacroEvent{key(0x41, true, 0), launch(`C:\a.exe`, "--x"), text("gg\nwp")}},
		{ID: "2b", Name: "types", Mode: "once", Imported: true, Events: []MacroEvent{text("gg")}},
		{ID: "3c", Name: "unc", Mode: "once", Events: []MacroEvent{launch(`\\host\share\a.exe`, "")}},
		{ID: `"><img src=x>`, Name: "id", Mode: "once"},
		{ID: "", Name: "no id", Mode: "once"},
	}
	src.Bindings = []Binding{
		{Family: "F", Button: 5, Macro: "1a", Trigger: firstTrigger},
		{Family: "F", Button: 4, Macro: "3c", Trigger: firstTrigger + 1},
	}
	b, _ := json.Marshal(src)
	c, err := importConfig(b)
	if err != nil || len(c.Macros) != 2 || len(c.Bindings) != 1 || c.Bindings[0].Macro != "1a" {
		t.Fatalf("%v %+v %+v", err, c.Macros, c.Bindings)
	}
	if !c.Macros[0].Imported || !slices.Equal(c.Macros[0].Events, src.Macros[0].Events) {
		t.Fatalf("a macro with a launch step: %+v", c.Macros[0])
	}
	if c.Macros[1].Imported || !slices.Equal(c.Macros[1].Events, src.Macros[1].Events) {
		t.Fatalf("a macro without a launch step: %+v", c.Macros[1])
	}
	// What this release exports imports the same, still blocked.
	b, _ = json.Marshal(c)
	again, err := importConfig(b)
	if err != nil || len(again.Macros) != 2 || !again.Macros[0].Imported || !slices.Equal(again.Macros[0].Events, c.Macros[0].Events) {
		t.Fatalf("%v %+v", err, again.Macros)
	}
}

// A backup with two macros of one ID: the second, which opens a program,
// must not reach the page. Saving it there would replace the first one, which
// carries no flag, and the bound button would then open the program unasked.
func TestImportConfigDropsDuplicateIDs(t *testing.T) {
	src := defaultConfig()
	src.Macros = []Macro{
		{ID: "a1", Name: "keys", Mode: "once", Events: []MacroEvent{key(0x41, true, 0), key(0x41, false, 0)}},
		{ID: "a1", Name: "opens", Mode: "once", Events: []MacroEvent{launch(`C:\a.exe`, "")}},
	}
	src.Bindings = []Binding{{Family: "F", Button: 5, Macro: "a1", Trigger: firstTrigger}}
	b, _ := json.Marshal(src)
	c, err := importConfig(b)
	if err != nil || len(c.Macros) != 1 || c.Macros[0].Name != "keys" || len(c.Bindings) != 1 {
		t.Fatalf("%v %+v %+v", err, c.Macros, c.Bindings)
	}
}

// Macros from an earlier release load and are written back as they were, and
// the new steps are written so that an earlier release sends no key for them.
func TestLoadConfigKeepsOldMacros(t *testing.T) {
	const old = `[{"id":"18a3f09c2b7d4e10","name":"m","events":[{"kind":"key","code":65,"down":true,"x":0,"y":0,"delay":0},{"kind":"move","code":0,"down":false,"x":3,"y":-4,"delay":20}],"mode":"repeat","count":2}]`
	c, unread, err := parseConfig([]byte(`{"macros":`+old+`}`), defaultConfig())
	if err != nil || len(unread) != 0 || len(c.Macros) != 1 {
		t.Fatalf("%v %v %+v", err, unread, c.Macros)
	}
	if b, _ := json.Marshal(c.Macros); string(b) != old {
		t.Fatalf("an old macro is written as\n%s\nwant\n%s", b, old)
	}
	m := Macro{ID: "1a", Name: "m", Mode: "once", Events: []MacroEvent{
		{Kind: "text", Text: "gg", Code: 0x41, Down: true, X: 1, Y: 1},
		{Kind: "launch", Target: `C:\a.exe`, Args: "--x", Code: 0x41, Down: true, X: 1, Y: 1},
	}}
	if err := m.normalize(); err != nil {
		t.Fatal(err)
	}
	const want = `[{"kind":"text","code":0,"down":false,"x":0,"y":0,"delay":0,"text":"gg"},{"kind":"launch","code":0,"down":false,"x":0,"y":0,"delay":0,"target":"C:\\a.exe","args":"--x"}]`
	if b, _ := json.Marshal(m.Events); string(b) != want {
		t.Fatalf("new steps are written as\n%s\nwant\n%s", b, want)
	}
	// After a trip through an earlier release the steps have a kind and
	// nothing else: the macro loads and does not play.
	c, _, err = parseConfig([]byte(`{"macros":[{"id":"1a","name":"m","mode":"once","count":1,"events":[{"kind":"key","code":65,"down":true},{"kind":"launch"}]}]}`), defaultConfig())
	if err != nil || len(c.Macros) != 1 {
		t.Fatalf("%v %+v", err, c.Macros)
	}
	s := recordSteps(t)
	play(c.Macros[0], make(chan struct{}))
	if len(s.entries()) != 0 || !slices.Equal(s.notices, []string{"macro.invalid"}) {
		t.Fatalf("an emptied launch step: played %q, notices %q", s.entries(), s.notices)
	}
}

// A failed launch is logged with the macro's name and the Windows error, and
// the step's contents stay out of the log.
func TestLaunchFailureLogsNoStepContents(t *testing.T) {
	s := recordSteps(t)
	s.fail = errors.New("not a Windows error")
	if runLaunch(Macro{Name: "m"}, launch(`C:\secret\app.exe`, "--token abc")) {
		t.Fatal("the macro went on after a failed launch")
	}
	if !slices.Equal(s.notices, []string{"macro.failed"}) {
		t.Fatalf("notices %q", s.notices)
	}
	for _, lang := range []string{"zh-CN", "en", "ja"} {
		for _, k := range []string{"macro.blocked", "macro.invalid", "macro.failed"} {
			if strings.Contains(messages[lang][k], "%") {
				t.Errorf("%s %s takes an argument: a notification names the macro only in its title", lang, k)
			}
		}
	}
}

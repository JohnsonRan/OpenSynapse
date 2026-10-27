package main

import (
	"slices"
	"sync"
	"testing"
	"time"
	"unsafe"

	"opensynapse/internal/razer"
)

func TestInputLayout(t *testing.T) {
	if unsafe.Sizeof(keyInput{}) != 40 || unsafe.Sizeof(mouseInput{}) != 40 {
		t.Fatal("INPUT must be 40 bytes on amd64")
	}
}

func TestTriggers(t *testing.T) {
	if a := triggerAssignment(firstTrigger); a.Fn != razer.FnKey || !slices.Equal(a.Data, []byte{0, 0x68}) {
		t.Fatalf("F13 = %+v", a) // the bytes the DeathAdder V4 Pro turned into F13
	}
	if !isTriggerAssignment(triggerAssignment(firstTrigger+11)) || isTriggerAssignment(razer.Assignment{Fn: razer.FnMouse, Data: []byte{5}}) {
		t.Fatal("isTriggerAssignment")
	}
	bs := []Binding{{Trigger: firstTrigger}, {Trigger: firstTrigger + 2}}
	if freeTrigger(bs) != firstTrigger+1 {
		t.Fatal("freeTrigger")
	}
	for i := range numTriggers {
		bs = append(bs, Binding{Trigger: firstTrigger + i})
	}
	if freeTrigger(bs) != 0 {
		t.Fatal("all used")
	}
}

func key(code int, down bool, delay int) MacroEvent { return MacroEvent{"key", code, down, delay} }
func btn(code int, down bool, delay int) MacroEvent { return MacroEvent{"mouse", code, down, delay} }

func TestCleanRecording(t *testing.T) {
	got := cleanRecording([]MacroEvent{
		key(0x41, false, 0), // released before recording started
		key(0x42, true, 300),
		key(0x42, false, 50),
		btn(1, true, 400), // the click on the stop button
		btn(1, false, 80),
	})
	want := []MacroEvent{key(0x42, true, 0), key(0x42, false, 50)}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
}

// capture replaces sendEvent while a test runs.
func capture(t *testing.T) func() []MacroEvent {
	var mu sync.Mutex
	var got []MacroEvent
	old := sendEvent
	sendEvent = func(e MacroEvent) { mu.Lock(); got = append(got, e); mu.Unlock() }
	t.Cleanup(func() { sendEvent = old })
	return func() []MacroEvent { mu.Lock(); defer mu.Unlock(); return slices.Clone(got) }
}

func TestPlayRepeatAndRelease(t *testing.T) {
	got := capture(t)
	play(Macro{Mode: "repeat", Count: 2, Events: []MacroEvent{key(0x41, true, 0), key(0x41, false, 0)}}, make(chan struct{}))
	if len(got()) != 4 {
		t.Fatalf("repeat: %v", got())
	}
	// Stopped while a key is down: the key is released.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		play(Macro{Mode: "hold", Events: []MacroEvent{key(0x10, true, 0), key(0x41, true, 5000)}}, stop)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	close(stop)
	<-done
	ev := got()[4:]
	if len(ev) != 2 || ev[0] != key(0x10, true, 0) || ev[1].Down || ev[1].Code != 0x10 {
		t.Fatalf("release: %v", ev)
	}
}

func TestTriggerModes(t *testing.T) {
	got := capture(t)
	cfgMu.Lock()
	saved := cfg
	cfg.Macros = []Macro{{ID: "t", Name: "t", Mode: "toggle", Events: []MacroEvent{key(0x41, true, 1), key(0x41, false, 1)}}}
	cfg.Bindings = []Binding{{Macro: "t", Trigger: firstTrigger}}
	cfgMu.Unlock()
	t.Cleanup(func() { cfgMu.Lock(); cfg = saved; cfgMu.Unlock() })

	if onTriggerKey(0x41, true) || onTriggerKey(firstTrigger+1, true) {
		t.Fatal("only bound triggers are swallowed")
	}
	if !onTriggerKey(firstTrigger, true) {
		t.Fatal("bound trigger not swallowed")
	}
	onTriggerKey(firstTrigger, true) // key repeat: no effect
	onTriggerKey(firstTrigger, false)
	time.Sleep(60 * time.Millisecond)
	onTriggerKey(firstTrigger, true) // second press stops the toggle
	onTriggerKey(firstTrigger, false)
	time.Sleep(30 * time.Millisecond)
	n := len(got())
	if n < 4 {
		t.Fatalf("toggle did not repeat: %v", got())
	}
	time.Sleep(50 * time.Millisecond)
	if len(got()) != n {
		t.Fatal("toggle did not stop")
	}
	playMu.Lock()
	defer playMu.Unlock()
	if len(playing) != 0 {
		t.Fatal("run not cleaned up")
	}
}

func TestMacroNormalize(t *testing.T) {
	m := Macro{Name: " a ", Mode: "bogus"}
	if m.normalize() != nil || m.Name != "a" || m.Mode != "once" || m.Events == nil {
		t.Fatalf("%+v", m)
	}
	for _, bad := range []Macro{
		{Name: ""},
		{Name: "x", Mode: "repeat", Count: 0},
		{Name: "x", Events: []MacroEvent{btn(6, true, 0)}},
		{Name: "x", Events: []MacroEvent{{Kind: "wheel", Code: 1}}},
		{Name: "x", Events: []MacroEvent{key(0x41, true, -1)}},
	} {
		if bad.normalize() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

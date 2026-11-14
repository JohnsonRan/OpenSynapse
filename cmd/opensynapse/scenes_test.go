package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

// offConfig turns scenes and smart polling off, the state of a first run,
// with nothing left to put back.
func offConfig(t testing.TB) {
	t.Setenv("APPDATA", t.TempDir())
	cfgMu.Lock()
	saved := cfg
	cfg = defaultConfig()
	cfgMu.Unlock()
	lastPending = nil
	t.Cleanup(func() {
		cfgMu.Lock()
		cfg = saved
		cfgMu.Unlock()
		lastPending = nil
	})
}

// A round of the scene switcher with scenes and smart polling off: what the
// background process pays while the user uses neither.
func BenchmarkAutoRoundOff(b *testing.B) {
	offConfig(b)
	a := newAuto()
	a.round()
	b.ReportAllocs()
	for b.Loop() {
		a.round()
	}
}

// With scenes and smart polling off the switcher sleeps, but not while a
// device still waits for what it showed or played before a scene.
func TestAutoRoundIdleOnlyWithNothingToPutBack(t *testing.T) {
	offConfig(t)
	a := newAuto()
	if !a.round() {
		t.Fatal("not idle with both off and nothing to put back")
	}

	sceneKeyboards.Lock()
	sceneKeyboards.base["gone"] = kbShow{} // unplugged during a scene
	sceneKeyboards.Unlock()
	idle := a.round()
	sceneKeyboards.Lock()
	delete(sceneKeyboards.base, "gone")
	sceneKeyboards.Unlock()
	if idle {
		t.Fatal("idle while a keyboard waits for its lighting")
	}

	origSet := setSoundVolume
	setSoundVolume = func(string, int) error { return errors.New("busy") }
	sceneAudio.Lock()
	sceneAudio.volume.base["speakers"] = 40
	sceneAudio.Unlock()
	idle = a.round()
	sceneAudio.Lock()
	delete(sceneAudio.volume.base, "speakers")
	sceneAudio.Unlock()
	setSoundVolume = origSet
	if idle {
		t.Fatal("idle while a sound device waits for its volume")
	}

	cfgMu.Lock()
	cfg.Smart.Enabled = true
	cfgMu.Unlock()
	if a.round() {
		t.Fatal("idle with smart polling on")
	}
}

// The scene switcher saves the baselines every round; an unchanged one must
// cost nothing.
func TestSavePendingAudioUnchangedDoesNotEncode(t *testing.T) {
	offConfig(t)
	sceneAudio.Lock()
	defer sceneAudio.Unlock()
	sceneAudio.volume.base["speakers"] = 40
	defer func() {
		delete(sceneAudio.volume.base, "speakers")
		_ = savePendingAudio()
	}()
	if err := os.MkdirAll(filepath.Dir(pendingPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := savePendingAudio(); err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() { _ = savePendingAudio() }); n != 0 {
		t.Fatalf("an unchanged save allocates %v times", n)
	}
	sceneAudio.volume.base["speakers"] = 55
	if err := savePendingAudio(); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(pendingPath()); err != nil || !strings.Contains(string(b), `"speakers":55`) {
		t.Fatalf("file after a change: %s, %v", b, err)
	}
}

// A device a rescan closed during the round is neither done nor put on the
// 10 s retry: the next round applies the target to the device that replaced it.
func TestApplierLeavesAClosedDeviceToTheNextRound(t *testing.T) {
	a := applier{name: "test"}
	a.reset()
	devs := []*razer.Device{dev("BF", "Razer Test", true)}
	a.run(1, devs, func(*razer.Device) error { return razer.ErrClosed })
	if len(a.done) != 0 || len(a.retryAt) != 0 {
		t.Fatalf("after a closed device: done %v, retry %v; want neither", a.done, a.retryAt)
	}
	applied := 0
	a.run(1, devs, func(*razer.Device) error { applied++; return nil })
	if applied != 1 || a.done["BF"] != 1 {
		t.Fatalf("applied %d times, done %v; want the next round to apply", applied, a.done)
	}
}

func TestDebouncer(t *testing.T) {
	d := newDebouncer()
	var switches []int
	for _, s := range []int{0, -1, 0, 0, 0, -1, -1} {
		if d.observe(s) {
			switches = append(switches, d.current)
		}
	}
	if !slices.Equal(switches, []int{0, -1}) {
		t.Fatalf("switches = %v, want [0 -1]", switches)
	}
}

func TestLooksLikeGame(t *testing.T) {
	cases := []struct {
		exe        string
		fullscreen bool
		gpu        float64
		decode     float64
		want       bool
	}{
		{"elden.exe", true, 60, 0, true},      // fullscreen + GPU
		{"elden.exe", true, 3, 0, false},      // light load
		{"elden.exe", true, -1, -1, false},    // not measured yet
		{"tvplayer.exe", true, 40, 4, false},  // unlisted player decoding video
		{"code.exe", false, 80, 0, false},     // windowed GPU load is not a game
		{"chrome.exe", true, 80, 0, false},    // browsers are excluded
		{"potplayer.exe", true, 80, 0, false}, // players are excluded
	}
	for _, k := range cases {
		if got := looksLikeGame(k.exe, k.fullscreen, k.gpu, k.decode); got != k.want {
			t.Errorf("%+v: got %v", k, got)
		}
	}
}

func TestGameStreak(t *testing.T) {
	var s gameStreak
	// A spike shorter than gameSeconds, broken by one quiet sample, is not a game.
	for range gameSeconds - 1 {
		if s.observe(1, true) {
			t.Fatal("game before gameSeconds samples")
		}
	}
	if s.observe(1, false) {
		t.Fatal("game on a quiet sample")
	}
	// A new foreground process starts its own count.
	for range gameSeconds - 1 {
		s.observe(1, true)
	}
	if s.observe(2, true) {
		t.Fatal("count carried over to another process")
	}
	for range gameSeconds - 2 {
		s.observe(2, true)
	}
	if !s.observe(2, true) {
		t.Fatal("not a game after gameSeconds samples")
	}
}

func TestDecideScene(t *testing.T) {
	c := ScenesConfig{Game: Scene{Apps: []string{"windowed.exe"}, Titles: []string{"GeForce NOW"}}, Scenes: []Scene{
		{Name: "fps", Apps: []string{"cs2.exe", "chrome.exe"}},
		{Name: "slow", Apps: []string{"game2.exe"}, Titles: []string{"Budget.xlsx", "report"}},
		{Name: "later", Titles: []string{"REPORT", ""}}, // "" only in a file normalize refused
	}}
	cases := []struct {
		exe, title string
		game       bool
		scene      int
		why        string
	}{
		{"cs2.exe", "", false, 0, reasonRule},
		{"game2.exe", "", true, 1, reasonRule},                              // a custom scene beats detection
		{"elden.exe", "", true, gameScene, reasonGame},                      // detected
		{"windowed.exe", "", false, gameScene, reasonRule},                  // listed in the game scene, not detected
		{"code.exe", "", false, desktop, reasonDefault},                     // and a window without a title matches no title rule
		{"cs2.exe", "budget.XLSX - Excel", false, 1, reasonRule},            // a title beats a program, without case
		{"code.exe", "Annual Report", false, 1, reasonRule},                 // the first scene with the title
		{"chrome.exe", "Play on GeForce NOW", false, gameScene, reasonRule}, // the game scene's title beats a custom scene's program
		{"chrome.exe", "GeForce NOW: Budget.xlsx", true, 1, reasonRule},     // a custom scene's title beats the game scene's
		{"chrome.exe", "New Tab", false, 0, reasonRule},                     // no title matches: by program
		{"code.exe", "untitled", false, desktop, reasonDefault},             // an empty rule matches no title
		{"elden.exe", "ELDEN RING", true, gameScene, reasonGame},
	}
	for _, k := range cases {
		if s, why := decideScene(c, noScene, k.exe, k.title, k.game); s != k.scene || why != k.why {
			t.Errorf("%+v: got %d %s", k, s, why)
		}
		// A locked scene stays whatever is in front: the desktop under a
		// detected game, a custom scene under another scene's title.
		for _, lock := range []int{desktop, gameScene, 2} {
			if s, why := decideScene(c, lock, k.exe, k.title, k.game); s != lock || why != reasonLock {
				t.Errorf("%+v, locked to %d: got %d %s", k, lock, s, why)
			}
		}
	}
}

func TestDecideRate(t *testing.T) {
	c := SmartPoll{GameRate: 0, NormalRate: 1000, Exceptions: []Rule{
		{Exe: "osu!.exe", Rate: 4000}, {Exe: "overwatch.exe", Rate: 1000}, {Title: "GeForce NOW", Rate: 8000}, {Title: "Sheets", Rate: 125},
	}}
	cases := []struct {
		exe, title string
		game       bool
		rate       int
		why        string
	}{
		{"osu!.exe", "", false, 4000, reasonRule},
		{"overwatch.exe", "", true, 1000, reasonRule},                  // an exception beats detection
		{"elden.exe", "", true, 0, reasonGame},                         // 0 = highest
		{"code.exe", "", false, 1000, reasonDefault},                   // and a window without a title matches no title rule
		{"chrome.exe", "Play on geforce now", false, 8000, reasonRule}, // by title, without case
		{"osu!.exe", "osu! - Google Sheets", false, 125, reasonRule},   // a title beats a program
		{"elden.exe", "ELDEN RING", true, 0, reasonGame},
	}
	for _, k := range cases {
		if r, why := decideRate(c, k.exe, k.title, k.game); r != k.rate || why != k.why {
			t.Errorf("%+v: got %d %s", k, r, why)
		}
	}
	// A hand-edited file whose game rate is not valid keeps its exceptions as
	// they are (parseConfig): a rule without a program and a title matches nothing.
	loaded, _, err := parseConfig([]byte(`{"smartPoll":{"enabled":true,"gameRate":333,"normalRate":1000,"exceptions":[{"exe":"","rate":125}]}}`), defaultConfig())
	if err != nil || len(loaded.Smart.Exceptions) != 1 {
		t.Fatalf("%v %+v; want the rule loaded as the file has it", err, loaded.Smart)
	}
	for _, title := range []string{"", "Notepad"} {
		if r, why := decideRate(loaded.Smart, "code.exe", title, false); r != 1000 || why != reasonDefault {
			t.Errorf("empty rule, title %q: got %d %s", title, r, why)
		}
	}
	if wantTitle(ScenesConfig{}, loaded.Smart, noScene) {
		t.Error("the title is read for a rule without a text")
	}
}

func TestNormalize(t *testing.T) {
	s := Scene{Name: " 直播 ", Apps: []string{` C:\Games\CS2.EXE `, "", "cs2.exe"}, Titles: []string{" GeForce NOW ", "", "geforce now", "  ", "Budget.xlsx"}}
	if err := s.normalize(); err != nil || s.Name != "直播" || !slices.Equal(s.Apps, []string{"cs2.exe"}) {
		t.Fatalf("%v %+v", err, s)
	}
	if !slices.Equal(s.Titles, []string{"GeForce NOW", "Budget.xlsx"}) {
		t.Fatalf("titles %q; want them trimmed, without empty ones and repeats, as the user spelled them", s.Titles)
	}
	sc := ScenesConfig{Desktop: Scene{Titles: []string{"x"}}, Game: Scene{Titles: []string{"y"}}}
	if err := sc.normalize(); err != nil || sc.Desktop.Titles != nil || !slices.Equal(sc.Game.Titles, []string{"y"}) {
		t.Fatalf("%v: desktop titles %q, game titles %q; want none on the desktop scene", err, sc.Desktop.Titles, sc.Game.Titles)
	}
	for _, bad := range []Scene{{Name: "a", DPI: 50}, {Name: ""}, {Name: "a", Lighting: &Lighting{Effect: "disco"}}} {
		if bad.normalize() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	p := SmartPoll{GameRate: 0, NormalRate: 1000, Exceptions: []Rule{
		{Exe: ` D:\osu!.EXE`, Rate: 4000}, {Exe: "osu!.exe", Rate: 500}, {Rate: 8000}, {Title: "  ", Rate: 8000},
		{Title: " GeForce NOW ", Rate: 8000}, {Title: "geforce now", Rate: 125}, {Exe: "a.exe", Title: "both", Rate: 500},
	}}
	want := []Rule{{Exe: "osu!.exe", Rate: 4000}, {Title: "GeForce NOW", Rate: 8000}, {Exe: "a.exe", Rate: 500}}
	if err := p.normalize(); err != nil || !slices.Equal(p.Exceptions, want) {
		t.Fatalf("%v %+v; want %+v", err, p.Exceptions, want)
	}
	for _, bad := range []SmartPoll{{GameRate: 333, NormalRate: 1000}, {NormalRate: 1000, Exceptions: []Rule{{Exe: "a.exe", Rate: 7}}}, {NormalRate: 1000, Exceptions: []Rule{{Title: "x", Rate: 7}}}} {
		if bad.normalize() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestLowBattery(t *testing.T) {
	var l = lowBattery{warned: map[string]bool{}}
	got := []bool{
		l.check("m", 30, 15, false), // fine
		l.check("m", 15, 15, false), // warn
		l.check("m", 12, 15, false), // already warned
		l.check("m", 12, 15, true),  // charging re-arms
		l.check("m", 14, 15, false), // warn again
	}
	if !slices.Equal(got, []bool{false, true, false, false, true}) {
		t.Fatal(got)
	}
}

// A saved scene must not inherit fields from the default scene it replaces.
func TestParseConfigDoesNotMergeDefaults(t *testing.T) {
	defaults := cfg
	defaults.Scenes.Scenes = []Scene{{Name: "default", Apps: []string{"x.exe"}, DPI: 800}}
	c, _, _ := parseConfig([]byte(`{"scenes":{"enabled":true,"desktop":{"name":"d"},"scenes":[{"name":"t","apps":["notepad.exe"]}]}}`), defaults)
	if len(c.Scenes.Scenes) != 1 || c.Scenes.Scenes[0].DPI != 0 || !c.Notify {
		t.Fatalf("%+v", c)
	}
	if c, _, _ := parseConfig([]byte(`{"notify":false}`), defaults); c.Notify || len(c.Scenes.Scenes) != 1 || c.Scenes.Scenes[0].DPI != 800 {
		t.Fatalf("defaults lost: %+v", c)
	}
}

// The first smart polling config (no auto-detection fields) becomes SmartPoll.
func TestLegacySmartMigration(t *testing.T) {
	c, _, _ := parseConfig([]byte(`{"smart":{"enabled":true,"defaultRate":1000,"rules":[{"exe":"cs2.exe","rate":8000}]}}`), cfg)
	s := c.Smart
	if !s.Enabled || s.NormalRate != 1000 || s.GameRate != 8000 || !slices.Equal(s.Exceptions, []Rule{{Exe: "cs2.exe", Rate: 8000}}) {
		t.Fatalf("%+v", s)
	}
	if c.Scenes.Enabled {
		t.Fatal("scenes must stay off after migration")
	}
}

func drainAutoReset() bool {
	select {
	case <-autoReset:
		return true
	default:
		return false
	}
}

// Saving a preset must not re-apply the scene's DPI, lighting and polling
// rate; the scene's audio picks the new EQ up on its own.
func TestSavePresetKeepsSwitchers(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	saved := cfg
	defer func() { cfg = saved }()
	cfg = defaultConfig()
	drainAutoReset()
	if err := (Audio{}).SavePreset(EQPreset{Name: "Music", EQ: flatEQ()}); err != nil {
		t.Fatal(err)
	}
	if drainAutoReset() {
		t.Fatal("saving a preset reset the scene switchers")
	}
}

func TestIsDecodeEngine(t *testing.T) {
	for n, want := range map[string]bool{
		"pid_42_luid_0x0_0x1_phys_0_eng_3_engtype_VideoDecode":    true, // NVIDIA
		"pid_42_luid_0x0_0x2_phys_0_eng_5_engtype_Video Decode 1": true, // AMD
		"pid_42_luid_0x0_0x2_phys_0_eng_6_engtype_Video Codec 0":  true, // AMD, decode and encode
		"pid_42_luid_0x0_0x1_phys_0_eng_4_engtype_VideoEncode":    false,
		"pid_42_luid_0x0_0x1_phys_0_eng_7_engtype_JPEG_Decode_1":  false,
		"pid_42_luid_0x0_0x2_phys_0_eng_8_engtype_Video JPEG 0":   false,
		"pid_42_luid_0x0_0x1_phys_0_eng_0_engtype_3D":             false,
	} {
		if isDecodeEngine(n) != want {
			t.Errorf("%s: got %v", n, !want)
		}
	}
}

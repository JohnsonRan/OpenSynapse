package main

import (
	"slices"
	"testing"
)

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
	c := ScenesConfig{Game: Scene{Apps: []string{"windowed.exe"}}, Scenes: []Scene{
		{Name: "fps", Apps: []string{"cs2.exe"}},
		{Name: "slow", Apps: []string{"game2.exe"}},
	}}
	cases := []struct {
		exe   string
		game  bool
		scene int
		why   string
	}{
		{"cs2.exe", false, 0, reasonRule},
		{"game2.exe", true, 1, reasonRule},             // a custom scene beats detection
		{"elden.exe", true, gameScene, reasonGame},     // detected
		{"windowed.exe", false, gameScene, reasonRule}, // listed in the game scene, not detected
		{"code.exe", false, desktop, reasonDefault},
	}
	for _, k := range cases {
		if s, why := decideScene(c, k.exe, k.game); s != k.scene || why != k.why {
			t.Errorf("%+v: got %d %s", k, s, why)
		}
	}
}

func TestDecideRate(t *testing.T) {
	c := SmartPoll{GameRate: 0, NormalRate: 1000, Exceptions: []Rule{{"osu!.exe", 4000}, {"overwatch.exe", 1000}}}
	cases := []struct {
		exe  string
		game bool
		rate int
		why  string
	}{
		{"osu!.exe", false, 4000, reasonRule},
		{"overwatch.exe", true, 1000, reasonRule}, // an exception beats detection
		{"elden.exe", true, 0, reasonGame},        // 0 = highest
		{"code.exe", false, 1000, reasonDefault},
	}
	for _, k := range cases {
		if r, why := decideRate(c, k.exe, k.game); r != k.rate || why != k.why {
			t.Errorf("%+v: got %d %s", k, r, why)
		}
	}
}

func TestNormalize(t *testing.T) {
	s := Scene{Name: " 直播 ", Apps: []string{` C:\Games\CS2.EXE `, "", "cs2.exe"}}
	if err := s.normalize(); err != nil || s.Name != "直播" || !slices.Equal(s.Apps, []string{"cs2.exe"}) {
		t.Fatalf("%v %+v", err, s)
	}
	for _, bad := range []Scene{{Name: "a", DPI: 50}, {Name: ""}, {Name: "a", Lighting: &Lighting{Effect: "disco"}}} {
		if bad.normalize() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	p := SmartPoll{GameRate: 0, NormalRate: 1000, Exceptions: []Rule{{` D:\osu!.EXE`, 4000}, {"osu!.exe", 500}, {"", 8000}}}
	if err := p.normalize(); err != nil || len(p.Exceptions) != 1 || p.Exceptions[0] != (Rule{"osu!.exe", 4000}) {
		t.Fatalf("%v %+v", err, p)
	}
	for _, bad := range []SmartPoll{{GameRate: 333, NormalRate: 1000}, {NormalRate: 1000, Exceptions: []Rule{{"a.exe", 7}}}} {
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
	c, _ := parseConfig([]byte(`{"scenes":{"enabled":true,"desktop":{"name":"d"},"scenes":[{"name":"t","apps":["notepad.exe"]}]}}`), defaults)
	if len(c.Scenes.Scenes) != 1 || c.Scenes.Scenes[0].DPI != 0 || !c.Notify {
		t.Fatalf("%+v", c)
	}
	if c, _ := parseConfig([]byte(`{"notify":false}`), defaults); c.Notify || len(c.Scenes.Scenes) != 1 || c.Scenes.Scenes[0].DPI != 800 {
		t.Fatalf("defaults lost: %+v", c)
	}
}

// The first smart polling config (no auto-detection fields) becomes SmartPoll.
func TestLegacySmartMigration(t *testing.T) {
	c, _ := parseConfig([]byte(`{"smart":{"enabled":true,"defaultRate":1000,"rules":[{"exe":"cs2.exe","rate":8000}]}}`), cfg)
	s := c.Smart
	if !s.Enabled || s.NormalRate != 1000 || s.GameRate != 8000 || !slices.Equal(s.Exceptions, []Rule{{"cs2.exe", 8000}}) {
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

package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/JohnsonRan/OpenSynapse/internal/leobog"
	"github.com/JohnsonRan/OpenSynapse/internal/razer"
)

// Lighting is a whole-device lighting choice.
type Lighting struct {
	Effect string    `json:"effect"` // off, static, breathing, spectrum, wave
	Color  razer.RGB `json:"color"`
}

// Scene is a set of device settings used while one of its programs, or a
// window whose title holds one of its texts, is in front. Zero/nil fields
// leave that setting alone.
type Scene struct {
	Name string   `json:"name"`
	Apps []string `json:"apps"` // program file names, e.g. cs2.exe
	// Titles: texts of a window title, compared without case. A release that
	// does not know the key reads the scene as one without them, so it never
	// uses the scene more widely than asked.
	Titles     []string  `json:"titles,omitempty"`
	DPI        int       `json:"dpi"`
	Lighting   *Lighting `json:"lighting"`   // every device with lighting
	Brightness *int      `json:"brightness"` // 0–100
	// Audio: unset (nil, "") puts back what played before a scene changed it.
	EQ     *SceneEQ `json:"eq"`     // audio dongles
	Output string   `json:"output"` // default playback device ID
	Input  string   `json:"input"`  // default recording device ID
	Volume *int     `json:"volume"` // 0–100, of the playback device in use
	// Keyboard: LEOBOG keyboards' lighting; unset puts back what they showed before.
	Keyboard *leobog.Lighting `json:"keyboard"`
	// Highlight lights chosen keys of LEOBOG keyboards instead of Keyboard.
	Highlight *SceneHighlight `json:"highlight"`
}

// SceneHighlight lights some keys in one color and the others in another,
// through the keyboard's custom effect.
type SceneHighlight struct {
	Keys  []int     `json:"keys"` // matrix indexes
	Color razer.RGB `json:"color"`
	Rest  razer.RGB `json:"rest"` // every other key
}

func (h *SceneHighlight) colors() leobog.KeyColors {
	var c leobog.KeyColors
	for i := range c {
		c[i] = h.Rest
	}
	for _, k := range h.Keys {
		c[k] = h.Color
	}
	return c
}

// ScenesConfig: the first custom scene matching the foreground window wins,
// then Game (auto-detected games, plus what it lists), then Desktop; the
// order is decideScene's.
type ScenesConfig struct {
	Enabled bool    `json:"enabled"`
	Game    Scene   `json:"game"`
	Desktop Scene   `json:"desktop"`
	Scenes  []Scene `json:"scenes"` // custom, by program
}

// Rule is a polling rate (0 = the mouse's highest) for one program, or with
// no Exe for the windows whose title holds Title, compared without case.
type Rule struct {
	Exe   string `json:"exe"`
	Title string `json:"title,omitempty"`
	Rate  int    `json:"rate"`
}

// SmartPoll switches mouse polling rates: high while a game is in front,
// lower otherwise to save CPU and battery. Rates of 0 mean each mouse's
// highest; a rate above what a mouse supports becomes its highest.
type SmartPoll struct {
	Enabled    bool   `json:"enabled"`
	GameRate   int    `json:"gameRate"`
	NormalRate int    `json:"normalRate"`
	Exceptions []Rule `json:"exceptions"` // checked first
}

type config struct {
	Smart      SmartPoll           `json:"smartPoll"`
	Scenes     ScenesConfig        `json:"scenes"`
	Lighting   map[string]Lighting `json:"lighting"`   // by device ID; devices cannot report their effect
	Notify     bool                `json:"notify"`     // low-battery notifications
	Language   string              `json:"language"`   // "" follows the system
	Glass      bool                `json:"glass"`      // glass theme (glass.go)
	DPIDisplay bool                `json:"dpiDisplay"` // on-screen DPI when the DPI button changes it
	Macros     []Macro             `json:"macros"`
	Bindings   []Binding           `json:"bindings"`  // device buttons that play macros
	EQPresets  []EQPreset          `json:"eqPresets"` // the user's, for audio dongles
	// Keyboards: by device ID, what this PC's LEOBOG keyboards were set to; not taken from imports.
	Keyboards map[string]KeyboardMemory `json:"keyboards,omitempty"`
	// ViaKeys: by device ID, what a VIA keyboard's keys did before the app changed
	// them, by "layer:index"; not taken from imports.
	ViaKeys map[string]map[string]uint16 `json:"viaKeys,omitempty"`
	// ViaModels: by device ID, the model picked among those sharing its USB IDs.
	ViaModels map[string]string `json:"viaModels,omitempty"`
}

var (
	cfgMu       sync.Mutex
	cfg         = defaultConfig()
	saveMu      sync.Mutex
	saveBlocked bool
)

// defaultConfig is the configuration of a first run.
func defaultConfig() config {
	return config{
		Notify:     true,
		DPIDisplay: true,
		Smart:      SmartPoll{GameRate: 0, NormalRate: 1000, Exceptions: []Rule{}},
		Scenes: ScenesConfig{
			Game:    Scene{Name: "game", Apps: []string{}},
			Desktop: Scene{Name: "desktop", Apps: []string{}},
			Scenes:  []Scene{},
		},
		Macros:    []Macro{},
		Bindings:  []Binding{},
		EQPresets: []EQPreset{},
	}
}

func lightingFor(d *razer.Device) Lighting {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if l, ok := cfg.Lighting[d.ID]; ok {
		return l
	}
	return Lighting{Effect: "", Color: razer.RGB{R: 0x44, G: 0xD6, B: 0x2C}}
}

// rememberLighting records the effect a device shows, in memory; it is saved
// with the next config write.
func rememberLighting(id string, l Lighting) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if cfg.Lighting == nil {
		cfg.Lighting = map[string]Lighting{}
	}
	cfg.Lighting[id] = l
}

// portableDir is the folder of the portable version, which a file named
// "portable" next to the executable marks, or "". Its settings stay in it.
var portableDir = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(dir, "portable")); err != nil {
		return ""
	}
	return dir
}()

func configPath() string {
	if portableDir != "" {
		return filepath.Join(portableDir, "config.json")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "OpenSynapse", "config.json")
}

func saveConfig() error {
	saveMu.Lock()
	defer saveMu.Unlock()
	if saveBlocked {
		return errors.New(tr("err.saveBlocked", configPath()))
	}
	cfgMu.Lock()
	b, _ := json.MarshalIndent(cfg, "", "  ")
	cfgMu.Unlock()
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p) // atomic: a crash never leaves a half-written config
}

func validRate(hz int) bool {
	return slices.Contains([]int{125, 500, 1000, 2000, 4000, 8000}, hz)
}

var validEffects = []string{razer.EffectOff, razer.EffectStatic, razer.EffectBreathing, razer.EffectSpectrum, razer.EffectWave}

func (s *Scene) normalize() error {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return errors.New(tr("err.sceneName"))
	}
	if s.DPI != 0 && (s.DPI < 100 || s.DPI > 50000) {
		return errors.New(tr("err.dpi", s.Name))
	}
	if s.Lighting != nil && !slices.Contains(validEffects, s.Lighting.Effect) {
		return errors.New(tr("err.effect", s.Name))
	}
	if s.Brightness != nil && (*s.Brightness < 0 || *s.Brightness > 100) {
		return errors.New(tr("err.brightness", s.Name))
	}
	if s.Volume != nil && (*s.Volume < 0 || *s.Volume > 100) {
		return errors.New(tr("err.volume", s.Name))
	}
	if s.EQ != nil && s.EQ.EQ.Check() != nil {
		return errors.New(tr("err.sceneEQ", s.Name))
	}
	if s.Keyboard != nil && s.Keyboard.Check() != nil {
		return errors.New(tr("err.sceneKeyboard", s.Name))
	}
	if h := s.Highlight; h != nil {
		h.Keys = slices.Compact(slices.Sorted(slices.Values(h.Keys)))
		if slices.ContainsFunc(h.Keys, func(k int) bool { return k < 0 || k >= leobog.Keys }) {
			return errors.New(tr("err.sceneKeyboard", s.Name))
		}
	}
	apps := []string{}
	for _, a := range s.Apps {
		a = normExe(a)
		if a != "" && !slices.Contains(apps, a) {
			apps = append(apps, a)
		}
	}
	s.Apps = apps
	var titles []string
	for _, t := range s.Titles {
		t = strings.TrimSpace(t)
		if t != "" && !slices.ContainsFunc(titles, func(o string) bool { return strings.EqualFold(o, t) }) {
			titles = append(titles, t)
		}
	}
	s.Titles = titles
	return nil
}

func (c *SmartPoll) normalize() error {
	if !rateOrMax(c.GameRate) || !rateOrMax(c.NormalRate) {
		return errors.New(tr("err.rate"))
	}
	out := []Rule{}
	for _, r := range c.Exceptions {
		r.Exe, r.Title = normExe(r.Exe), strings.TrimSpace(r.Title)
		if r.Exe != "" {
			r.Title = "" // a release without title rules reads this rule as the program's too
		}
		if r.Exe == "" && r.Title == "" || slices.ContainsFunc(out, func(o Rule) bool {
			return o.Exe == r.Exe && strings.EqualFold(o.Title, r.Title)
		}) {
			continue
		}
		if !rateOrMax(r.Rate) {
			return errors.New(tr("err.ruleRate", cmp.Or(r.Exe, r.Title)))
		}
		out = append(out, r)
	}
	c.Exceptions = out
	return nil
}

func rateOrMax(hz int) bool { return hz == 0 || validRate(hz) }

func normExe(a string) string {
	a = strings.ToLower(strings.TrimSpace(a))
	if strings.ContainsAny(a, `\/`) {
		a = filepath.Base(a)
	}
	return a
}

func (c *ScenesConfig) normalize() error {
	c.Game.Name, c.Desktop.Name, c.Desktop.Apps, c.Desktop.Titles = "game", "desktop", nil, nil
	if err := c.Game.normalize(); err != nil {
		return err
	}
	if err := c.Desktop.normalize(); err != nil {
		return err
	}
	if c.Scenes == nil {
		c.Scenes = []Scene{}
	}
	for i := range c.Scenes {
		if err := c.Scenes[i].normalize(); err != nil {
			return err
		}
	}
	return nil
}

func (c ScenesConfig) clone() ScenesConfig {
	b, _ := json.Marshal(c)
	var out ScenesConfig
	_ = json.Unmarshal(b, &out)
	return out
}

package main

import (
	"encoding/json"
	"errors"
	"log"
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

// Scene is a set of device settings used while its programs are in front.
// Zero/nil fields leave that setting alone.
type Scene struct {
	Name       string    `json:"name"`
	Apps       []string  `json:"apps"` // program file names, e.g. cs2.exe
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

// ScenesConfig: the first custom scene listing the foreground program wins,
// then Game (auto-detected games, plus the programs it lists), then Desktop.
type ScenesConfig struct {
	Enabled bool    `json:"enabled"`
	Game    Scene   `json:"game"`
	Desktop Scene   `json:"desktop"`
	Scenes  []Scene `json:"scenes"` // custom, by program
}

// Rule is a per-program polling rate (0 = the mouse's highest).
type Rule struct {
	Exe  string `json:"exe"`
	Rate int    `json:"rate"`
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

// legacySmart is the config of the smart polling-rate page that scenes replaced.
type legacySmart struct {
	Enabled     bool `json:"enabled"`
	DefaultRate int  `json:"defaultRate"`
	Rules       []struct {
		Exe  string `json:"exe"`
		Rate int    `json:"rate"`
	} `json:"rules"`
	AutoDetect bool `json:"autoDetect"`
	AutoRate   int  `json:"autoRate"`
}

// smartPoll converts the first smart polling-rate config.
func (l legacySmart) smartPoll() SmartPoll {
	if l.AutoRate == 0 {
		l.AutoRate = 8000 // the first version had no auto-detection fields
	}
	c := SmartPoll{Enabled: l.Enabled, GameRate: l.AutoRate, NormalRate: l.DefaultRate, Exceptions: []Rule{}}
	for _, r := range l.Rules {
		c.Exceptions = append(c.Exceptions, Rule{r.Exe, r.Rate})
	}
	return c
}

func loadConfig() {
	p := configPath()
	b, err := os.ReadFile(p)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("cannot read config %s: %v; saving disabled", p, err)
			saveMu.Lock()
			saveBlocked = true
			saveMu.Unlock()
			return
		}
		saveMu.Lock()
		saveBlocked = false
		saveMu.Unlock()
		return // first run: defaults
	}
	cfgMu.Lock()
	def := cfg
	cfgMu.Unlock()
	next, err := parseConfig(b, def)
	if err != nil {
		if renameErr := os.Rename(p, p+".bad"); renameErr != nil {
			log.Printf("corrupted config %s: %v; backup failed: %v; saving disabled", p, err, renameErr)
			saveMu.Lock()
			saveBlocked = true
			saveMu.Unlock()
			return
		}
		log.Printf("corrupted config %s: %v; backed up to %s.bad", p, err, p)
		saveMu.Lock()
		saveBlocked = false
		saveMu.Unlock()
		return
	}
	cfgMu.Lock()
	cfg = next
	cfgMu.Unlock()
	saveMu.Lock()
	saveBlocked = false
	saveMu.Unlock()
}

// parseConfig overlays the saved keys on the defaults. Each key is decoded on
// its own: decoding into the defaults would reuse their slice elements, so a
// saved scene would inherit fields it omits.
func parseConfig(b []byte, defaults config) (config, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return defaults, err
	}
	c := defaults
	if v, ok := raw["scenes"]; ok {
		var s ScenesConfig
		if json.Unmarshal(v, &s) == nil {
			_ = s.normalize() // fills names and lists an older save lacks
			c.Scenes = s
		}
	}
	if v, ok := raw["smartPoll"]; ok {
		var s SmartPoll
		if json.Unmarshal(v, &s) == nil {
			_ = s.normalize()
			c.Smart = s
		}
	} else if v, ok := raw["smart"]; ok {
		var l legacySmart
		if json.Unmarshal(v, &l) == nil {
			c.Smart = l.smartPoll()
			_ = c.Smart.normalize()
		}
	}
	if v, ok := raw["lighting"]; ok {
		_ = json.Unmarshal(v, &c.Lighting)
	}
	if v, ok := raw["notify"]; ok {
		_ = json.Unmarshal(v, &c.Notify)
	}
	if v, ok := raw["dpiDisplay"]; ok {
		_ = json.Unmarshal(v, &c.DPIDisplay)
	}
	if v, ok := raw["glass"]; ok {
		_ = json.Unmarshal(v, &c.Glass)
	}
	if v, ok := raw["language"]; ok {
		_ = json.Unmarshal(v, &c.Language)
	}
	if v, ok := raw["macros"]; ok {
		var m []Macro
		if json.Unmarshal(v, &m) == nil && m != nil {
			c.Macros = m
		}
	}
	if v, ok := raw["bindings"]; ok {
		var b []Binding
		if json.Unmarshal(v, &b) == nil && b != nil {
			c.Bindings = b
		}
	}
	if v, ok := raw["keyboards"]; ok {
		_ = json.Unmarshal(v, &c.Keyboards)
	}
	if v, ok := raw["viaKeys"]; ok {
		_ = json.Unmarshal(v, &c.ViaKeys)
	}
	if v, ok := raw["viaModels"]; ok {
		_ = json.Unmarshal(v, &c.ViaModels)
	}
	if v, ok := raw["eqPresets"]; ok {
		var p []EQPreset
		if json.Unmarshal(v, &p) == nil && p != nil {
			c.EQPresets = p
		}
	}
	return c, nil
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
	return nil
}

func (c *SmartPoll) normalize() error {
	if !rateOrMax(c.GameRate) || !rateOrMax(c.NormalRate) {
		return errors.New(tr("err.rate"))
	}
	out := []Rule{}
	for _, r := range c.Exceptions {
		r.Exe = normExe(r.Exe)
		if r.Exe == "" || slices.ContainsFunc(out, func(o Rule) bool { return o.Exe == r.Exe }) {
			continue
		}
		if !rateOrMax(r.Rate) {
			return errors.New(tr("err.ruleRate", r.Exe))
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
	c.Game.Name, c.Desktop.Name, c.Desktop.Apps = "game", "desktop", nil
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

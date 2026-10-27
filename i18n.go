package main

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

// Languages the app is written in. The page has its own strings (src/i18n.ts);
// these are the ones Go shows: tray menu, notifications, validation errors.
var messages = map[string]map[string]string{
	"zh-CN": {
		"tray.open":        "打开 OpenSynapse",
		"tray.smart":       "智能回报率",
		"tray.scenes":      "情景模式",
		"tray.quit":        "退出",
		"tray.wired":       "有线",
		"tray.wireless":    "无线",
		"tray.rate":        "回报率: %s",
		"tray.scene":       "情景: %s",
		"tray.moreDevices": "其他设备 (%d)",
		"rate.highest":     "最高",
		"scene.game":       "游戏",
		"scene.desktop":    "桌面",
		"battery.title":    "电量低: %s",
		"battery.body":     "剩余 %d%%，请尽快充电",
		"err.sceneName":    "请为每个情景填写名称",
		"err.dpi":          "%s: DPI 需为 100–50000",
		"err.effect":       "%s: 灯效无效",
		"err.brightness":   "%s: 亮度需为 0–100",
		"err.rate":         "回报率无效",
		"err.ruleRate":     "%s: 回报率无效",
		"err.language":     "不支持该语言",
	},
	"en": {
		"tray.open":        "Open OpenSynapse",
		"tray.smart":       "Smart polling rate",
		"tray.scenes":      "Scenes",
		"tray.quit":        "Quit",
		"tray.wired":       "wired",
		"tray.wireless":    "wireless",
		"tray.rate":        "Polling rate: %s",
		"tray.scene":       "Scene: %s",
		"tray.moreDevices": "More devices (%d)",
		"rate.highest":     "highest",
		"scene.game":       "Game",
		"scene.desktop":    "Desktop",
		"battery.title":    "Low battery: %s",
		"battery.body":     "%d%% left. Please charge it soon.",
		"err.sceneName":    "Every scene needs a name",
		"err.dpi":          "%s: DPI must be between 100 and 50000",
		"err.effect":       "%s: invalid lighting effect",
		"err.brightness":   "%s: brightness must be between 0 and 100",
		"err.rate":         "Invalid polling rate",
		"err.ruleRate":     "%s: invalid polling rate",
		"err.language":     "Unsupported language",
	},
}

// language resolves the configured language; "" follows the system: Chinese
// for any Chinese display language, English otherwise.
func language() string {
	cfgMu.Lock()
	l := cfg.Language
	cfgMu.Unlock()
	if _, ok := messages[l]; ok {
		return l
	}
	// The Windows display languages directly: mygo.App.PreferredLanguages
	// needs the running app, and validation also runs before it (and in tests).
	langs, _ := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	for _, p := range langs {
		if strings.HasPrefix(strings.ToLower(p), "zh") {
			return "zh-CN"
		}
		if strings.HasPrefix(strings.ToLower(p), "en") {
			return "en"
		}
	}
	return "en"
}

func tr(key string, args ...any) string {
	s := messages[language()][key]
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// LanguageInfo: Choice is what the user picked ("" = system), Effective what is shown.
type LanguageInfo struct {
	Choice    string `json:"choice"`
	Effective string `json:"effective"`
}

// Language returns the language setting.
func (Settings) Language() LanguageInfo {
	cfgMu.Lock()
	c := cfg.Language
	cfgMu.Unlock()
	return LanguageInfo{c, language()}
}

// SetLanguage sets "zh-CN", "en", or "" to follow the system.
func (Settings) SetLanguage(lang string) error {
	if _, ok := messages[lang]; !ok && lang != "" {
		return errors.New(tr("err.language"))
	}
	cfgMu.Lock()
	cfg.Language = lang
	cfgMu.Unlock()
	resetAuto() // the tray tooltip names scenes in the new language
	return saveConfig()
}

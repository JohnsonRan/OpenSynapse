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
		"tray.open":         "打开 OpenSynapse",
		"tray.smart":        "智能回报率",
		"tray.scenes":       "情景模式",
		"tray.quit":         "退出",
		"tray.wired":        "有线",
		"tray.wireless":     "无线",
		"tray.rate":         "回报率: %s",
		"tray.scene":        "情景: %s",
		"tray.moreDevices":  "其他设备 (%d)",
		"rate.highest":      "最高",
		"scene.game":        "游戏",
		"scene.desktop":     "桌面",
		"battery.title":     "电量低: %s",
		"battery.body":      "剩余 %d%%，请尽快充电",
		"err.sceneName":     "请为每个情景填写名称",
		"err.dpi":           "%s: DPI 需为 100–50000",
		"err.effect":        "%s: 灯效无效",
		"err.brightness":    "%s: 亮度需为 0–100",
		"err.sceneEQ":       "%s: 均衡器设置无效",
		"err.rate":          "回报率无效",
		"err.ruleRate":      "%s: 回报率无效",
		"err.language":      "不支持该语言",
		"err.macroName":     "请为宏填写名称",
		"err.macroCount":    "%s: 重复次数需为 1–999",
		"err.macroLong":     "%s: 步骤不能超过 2000 个",
		"err.macroEvent":    "%s: 有无效的步骤",
		"err.macroBound":    "有按键正在使用此宏，请先解除绑定",
		"err.macroMissing":  "找不到该宏",
		"err.leftButton":    "左键不能绑定宏",
		"err.noTrigger":     "最多只能绑定 %d 个按键",
		"err.buttonAction":  "无效的按键功能",
		"err.importFile":    "这不是 OpenSynapse 的设置文件",
		"err.noSoundDevice": "Windows 中没有这个设备的播放设备",
		"err.presetName":    "请为预设命名",
		"err.saveBlocked":   "设置未保存：无法读取 %s。为避免覆盖其中的设置，已暂停保存。请修复或移走该文件，然后重启 OpenSynapse。",
	},
	"en": {
		"tray.open":         "Open OpenSynapse",
		"tray.smart":        "Smart polling rate",
		"tray.scenes":       "Scenes",
		"tray.quit":         "Quit",
		"tray.wired":        "wired",
		"tray.wireless":     "wireless",
		"tray.rate":         "Polling rate: %s",
		"tray.scene":        "Scene: %s",
		"tray.moreDevices":  "More devices (%d)",
		"rate.highest":      "highest",
		"scene.game":        "Game",
		"scene.desktop":     "Desktop",
		"battery.title":     "Low battery: %s",
		"battery.body":      "%d%% left. Please charge it soon.",
		"err.sceneName":     "Every scene needs a name",
		"err.dpi":           "%s: DPI must be between 100 and 50000",
		"err.effect":        "%s: invalid lighting effect",
		"err.brightness":    "%s: brightness must be between 0 and 100",
		"err.sceneEQ":       "%s: the EQ isn't valid",
		"err.rate":          "Invalid polling rate",
		"err.ruleRate":      "%s: invalid polling rate",
		"err.language":      "Unsupported language",
		"err.macroName":     "Every macro needs a name",
		"err.macroCount":    "%s: repeat count must be between 1 and 999",
		"err.macroLong":     "%s: a macro can have at most 2000 steps",
		"err.macroEvent":    "%s: invalid step",
		"err.macroBound":    "A button plays this macro. Unbind it first.",
		"err.macroMissing":  "Macro not found",
		"err.leftButton":    "The left button can't play a macro",
		"err.noTrigger":     "At most %d buttons can play macros",
		"err.buttonAction":  "Invalid button function",
		"err.importFile":    "This isn't an OpenSynapse settings file",
		"err.noSoundDevice": "Windows has no playback device for this device",
		"err.presetName":    "Give the preset a name",
		"err.saveBlocked":   "Settings weren't saved: OpenSynapse can't read %s. To avoid overwriting the settings in it, saving is paused. Fix or move the file, and then restart OpenSynapse.",
	},
	// Japanese per the Microsoft Japanese style guide; see src/i18n.ts.
	"ja": {
		"tray.open":         "OpenSynapse を開く",
		"tray.smart":        "スマート ポーリングレート",
		"tray.scenes":       "シーン",
		"tray.quit":         "終了",
		"tray.wired":        "有線",
		"tray.wireless":     "ワイヤレス",
		"tray.rate":         "ポーリングレート: %s",
		"tray.scene":        "シーン: %s",
		"tray.moreDevices":  "その他のデバイス (%d)",
		"rate.highest":      "最高",
		"scene.game":        "ゲーム",
		"scene.desktop":     "デスクトップ",
		"battery.title":     "バッテリー低下: %s",
		"battery.body":      "残り %d%% です。早めに充電してください。",
		"err.sceneName":     "すべてのシーンに名前を付けてください",
		"err.dpi":           "%s: DPI は 100 から 50000 の範囲で指定してください",
		"err.effect":        "%s: エフェクトが無効です",
		"err.brightness":    "%s: 明るさは 0 から 100 の範囲で指定してください",
		"err.sceneEQ":       "%s: EQ の設定が無効です",
		"err.rate":          "ポーリングレートが無効です",
		"err.ruleRate":      "%s: ポーリングレートが無効です",
		"err.language":      "この言語はサポートされていません",
		"err.macroName":     "マクロに名前を付けてください",
		"err.macroCount":    "%s: 回数は 1 から 999 の範囲で指定してください",
		"err.macroLong":     "%s: ステップは 2000 個までです",
		"err.macroEvent":    "%s: 無効なステップがあります",
		"err.macroBound":    "このマクロはボタンに割り当てられています。先に割り当てを解除してください。",
		"err.macroMissing":  "マクロが見つかりません",
		"err.leftButton":    "左ボタンにはマクロを割り当てられません",
		"err.noTrigger":     "マクロを割り当てられるボタンは %d 個までです",
		"err.buttonAction":  "ボタンの機能が無効です",
		"err.importFile":    "OpenSynapse の設定ファイルではありません",
		"err.noSoundDevice": "このデバイスの再生デバイスが Windows にありません",
		"err.presetName":    "プリセットに名前を付けてください",
		"err.saveBlocked":   "設定は保存されませんでした。%s を読み取れません。その中の設定を上書きしないよう、保存を一時停止しています。ファイルを修正するか移動してから、OpenSynapse を再起動してください。",
	},
}

// language resolves the configured language; "" follows the system: Chinese
// for any Chinese display language, Japanese for Japanese, English otherwise.
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
		if strings.HasPrefix(strings.ToLower(p), "ja") {
			return "ja"
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

// SetLanguage sets "zh-CN", "en", "ja", or "" to follow the system.
func (Settings) SetLanguage(lang string) error {
	if _, ok := messages[lang]; !ok && lang != "" {
		return errors.New(tr("err.language"))
	}
	cfgMu.Lock()
	cfg.Language = lang
	cfgMu.Unlock()
	resetAuto()      // the tray tooltip names scenes in the new language
	go refreshTray() // and the device rows say wired/wireless in it
	return saveConfig()
}

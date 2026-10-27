// UI strings. zh is the source; en must have the same keys (the type checks it).
// Placeholders are written {name}.

const zh = {
  "nav.devices": "设备",
  "nav.scenes": "情景",
  "nav.scenesTitle": "情景模式",
  "nav.settings": "设置",

  "overview.eyebrow": "已连接",
  "overview.title": "设备",
  "overview.rescan": "重新扫描",
  "overview.none": "未检测到支持的设备。",
  "overview.noneHint": "确认设备已开机、接收器已插好，并退出 Razer Synapse，然后点击右上角重新扫描。",
  "overview.battery": "电量 {pct}%",
  "overview.noAnswer": "未响应",

  "cat.mouse": "鼠标",
  "cat.keyboard": "键盘",
  "cat.mouse-mat": "鼠标垫",
  "cat.accessory": "配件",
  "cat.monitor": "显示器",

  "device.back": "‹ 全部设备",
  "device.asleep": "未响应（可能已休眠）",
  "device.toWired": "已切换到有线连接",
  "device.toWireless": "已切换到无线连接",
  "device.gone": "{name} 已断开",

  "tab.perf": "性能",
  "tab.lighting": "灯光",
  "tab.power": "电源",
  "tab.receiver": "接收器",

  "dpi.title": "灵敏度",
  "dpi.current": "当前",
  "dpi.stagesHint": "点击档位即可切换灵敏度；设备上的 DPI 按钮会按顺序循环切换。",
  "dpi.addStage": "＋ 添加档位",
  "dpi.xyLink": "X/Y 同步",
  "dpi.deleteStage": "删除档位",

  "poll.title": "回报率",
  "poll.set": "回报率已设为 {hz} Hz",
  "poll.smartOn": "智能回报率已开启，将随前台程序自动切换",
  "poll.highCost": "高回报率会增加 CPU 占用和耗电",

  "light.effect": "灯效",
  "light.color": "颜色",
  "light.customColor": "自定义颜色",
  "light.brightness": "亮度",
  "effect.off": "关闭",
  "effect.static": "静态",
  "effect.breathing": "呼吸",
  "effect.spectrum": "光谱循环",
  "effect.wave": "波浪",

  "power.sleep": "休眠",
  "power.minutes": "分钟",
  "power.sleepHint": "闲置多久后进入休眠。",
  "power.sleepSaved": "休眠时间已保存",
  "power.low": "低电量提醒",
  "power.lowHint": "电量低于此值时进入省电模式并闪烁提醒。",
  "power.lowSaved": "低电量阈值已保存",
  "power.unreadable": "无法读取电量",
  "power.charging": "正在充电",
  "power.onBattery": "使用电池",

  "receiver.leds": "指示灯",
  "receiver.ledsHint": "分别设置接收器三颗指示灯的显示内容。",
  "receiver.ledsMore": "灯的颜色由固件决定，公开协议不支持修改。",
  "receiver.left": "左",
  "receiver.middle": "中",
  "receiver.right": "右",
  "receiver.saved": "指示灯已保存",
  "led.off": "关闭",
  "led.battery": "电量",
  "led.connection": "连接状态",
  "led.pollRate": "回报率",
  "led.dpi": "DPI",

  "auto.eyebrow": "自动切换",
  "auto.title": "情景模式",
  "auto.intro": "根据前台程序自动调整设备设置。",
  "auto.more": "切换前有约 2 秒防抖，窗口关闭时后台不加载界面。手动改过的设置会在下次切换时被覆盖。",
  "auto.allOff": "智能回报率和情景都已关闭",
  "auto.waiting": "等待前台程序…",
  "auto.foreground": "前台：{exe}",
  "auto.fullscreen": "全屏",
  "auto.windowed": "窗口",
  "auto.game": "识别为游戏",
  "auto.rate": "回报率：{rate}",
  "auto.scene": "情景：{name}",

  "smart.title": "智能回报率",
  "smart.intro": "游戏时提高所有鼠标的回报率，平时自动降低，省电也省 CPU。",
  "smart.more": "判定为游戏：窗口全屏或无边框全屏，且 GPU 3D 占用 ≥ 15%（浏览器和播放器除外）。“最高”即各鼠标自身的上限，所选数值超过上限时同样按上限设置。",
  "smart.gameRate": "游戏时",
  "smart.normalRate": "平时",
  "smart.highest": "最高",
  "smart.exceptions": "例外",
  "smart.exceptionsHint": "为指定程序固定回报率，优先于上面的规则。",
  "smart.add": "＋ 添加",
  "smart.exePlaceholder": "程序文件名，如 osu!.exe",
  "smart.noExceptions": "暂无例外。",

  "scenes.title": "情景",
  "scenes.intro": "按前台程序切换所有鼠标的 DPI 和带灯设备的灯光。",
  "scenes.more": "匹配顺序：自定义情景（按程序）→ 游戏 → 桌面。选“保持当前”的项不会被改动。",
  "scenes.add": "＋ 新建情景",
  "scenes.newName": "情景 {n}",
  "scenes.delete": "删除情景",
  "scenes.game": "游戏",
  "scenes.gameHint": "识别到游戏时启用。",
  "scenes.gameMore": "识别标准：全屏或无边框全屏，且 GPU 3D 占用 ≥ 15%。识别不到的游戏（如窗口模式运行）可在“程序”中补充。",
  "scenes.desktop": "桌面",
  "scenes.desktopHint": "以上都不匹配时使用。",
  "scenes.desktopMore": "离开其他情景时也由它恢复设置；在这里设置灯光，可让所有设备灯光同步。",
  "scenes.programs": "程序",
  "scenes.appsCustom": "程序文件名，多个用逗号分隔，如 obs64.exe, code.exe",
  "scenes.appsGame": "补充程序，如 windowed-game.exe（可留空）",
  "scenes.dpi": "DPI（鼠标）",
  "scenes.effect": "灯效（所有带灯设备）",
  "scenes.keep": "保持当前",
  "scenes.noPrograms": "未绑定程序",
  "scenes.nothingSet": "全部保持当前",
  "scenes.edit": "展开编辑",
  "scenes.brightnessToggle": "勾选后由情景调整亮度",

  "settings.title": "设置",
  "settings.language": "语言",
  "settings.languageHint": "界面和托盘菜单使用的语言。",
  "settings.langSystem": "跟随系统",
  "settings.autostart": "开机启动",
  "settings.autostartHint": "登录后以托盘方式启动，只在后台保持轻量运行。",
  "settings.autostartOn": "已开启开机启动",
  "settings.autostartOff": "已关闭开机启动",
  "settings.notify": "低电量通知",
  "settings.notifyHint": "无线设备电量低时弹出一次系统通知。",
  "settings.notifyMore": "电量低于设备自身的提醒阈值时通知（无阈值的设备按 20%）。悬停托盘图标可查看各设备电量。",
  "settings.notifyOn": "已开启低电量通知",
  "settings.notifyOff": "已关闭低电量通知",
  "settings.about": "关于",
  "settings.aboutText":
    "OpenSynapse 是一款开源、轻量的 Razer 外设设置工具，与 Razer Inc. 无关联。<br />关闭窗口后程序会留在托盘，右键托盘图标可退出。请勿与 Razer Synapse 同时运行，否则两者会互相覆盖设置。<br />协议资料来自 openrazer 项目的公开文档。",
};

type Key = keyof typeof zh;

const en: Record<Key, string> = {
  "nav.devices": "Devices",
  "nav.scenes": "Scenes",
  "nav.scenesTitle": "Scenes",
  "nav.settings": "Settings",

  "overview.eyebrow": "Connected",
  "overview.title": "Devices",
  "overview.rescan": "Scan again",
  "overview.none": "No supported device found.",
  "overview.noneHint": "Make sure the device is on and its receiver is plugged in, quit Razer Synapse, then click Scan again at the top right.",
  "overview.battery": "Battery {pct}%",
  "overview.noAnswer": "Not responding",

  "cat.mouse": "Mouse",
  "cat.keyboard": "Keyboard",
  "cat.mouse-mat": "Mouse mat",
  "cat.accessory": "Accessory",
  "cat.monitor": "Monitor",

  "device.back": "‹ All devices",
  "device.asleep": "Not responding (may be asleep)",
  "device.toWired": "Switched to the wired connection",
  "device.toWireless": "Switched to the wireless connection",
  "device.gone": "{name} disconnected",

  "tab.perf": "Performance",
  "tab.lighting": "Lighting",
  "tab.power": "Power",
  "tab.receiver": "Receiver",

  "dpi.title": "Sensitivity",
  "dpi.current": "Current",
  "dpi.stagesHint": "Click a stage to switch to it now. The DPI button on the device cycles through these stages in order.",
  "dpi.addStage": "+ Add stage",
  "dpi.xyLink": "Link X/Y",
  "dpi.deleteStage": "Delete stage",

  "poll.title": "Polling rate",
  "poll.set": "Polling rate set to {hz} Hz",
  "poll.smartOn": "Smart polling rate is on and follows the program in front",
  "poll.highCost": "Higher rates use more CPU and battery",

  "light.effect": "Effect",
  "light.color": "Color",
  "light.customColor": "Custom color",
  "light.brightness": "Brightness",
  "effect.off": "Off",
  "effect.static": "Static",
  "effect.breathing": "Breathing",
  "effect.spectrum": "Spectrum",
  "effect.wave": "Wave",

  "power.sleep": "Sleep",
  "power.minutes": "min",
  "power.sleepHint": "Idle time before the device sleeps.",
  "power.sleepSaved": "Sleep time saved",
  "power.low": "Low-battery warning",
  "power.lowHint": "Below this level the device saves power and blinks a warning.",
  "power.lowSaved": "Low-battery threshold saved",
  "power.unreadable": "Battery level unavailable",
  "power.charging": "Charging",
  "power.onBattery": "On battery",

  "receiver.leds": "Indicators",
  "receiver.ledsHint": "Choose what each of the three receiver indicators shows.",
  "receiver.ledsMore": "Their color is set by the firmware; the public protocol cannot change it.",
  "receiver.left": "Left",
  "receiver.middle": "Middle",
  "receiver.right": "Right",
  "receiver.saved": "Indicators saved",
  "led.off": "Off",
  "led.battery": "Battery",
  "led.connection": "Connection",
  "led.pollRate": "Polling rate",
  "led.dpi": "DPI",

  "auto.eyebrow": "Automatic",
  "auto.title": "Scenes",
  "auto.intro": "Adjusts your device settings to the program in front.",
  "auto.more": "A switch waits about 2 s; nothing is loaded while the window is closed. Settings you change by hand are overwritten at the next switch.",
  "auto.allOff": "Smart polling rate and scenes are off",
  "auto.waiting": "Waiting for a program…",
  "auto.foreground": "In front: {exe}",
  "auto.fullscreen": "fullscreen",
  "auto.windowed": "windowed",
  "auto.game": "game detected",
  "auto.rate": "polling rate: {rate}",
  "auto.scene": "scene: {name}",

  "smart.title": "Smart polling rate",
  "smart.intro": "Raise every mouse's polling rate while gaming, lower it otherwise to save battery and CPU.",
  "smart.more": "A game is a fullscreen or borderless-fullscreen window using at least 15% of the GPU 3D engine; browsers and players never count. \"Highest\" means each mouse's own maximum, and a value above a mouse's maximum uses that maximum.",
  "smart.gameRate": "While gaming",
  "smart.normalRate": "Otherwise",
  "smart.highest": "Highest",
  "smart.exceptions": "Exceptions",
  "smart.exceptionsHint": "Give a program a fixed polling rate. Exceptions come before the rules above.",
  "smart.add": "+ Add",
  "smart.exePlaceholder": "Program file name, e.g. osu!.exe",
  "smart.noExceptions": "No exceptions yet.",

  "scenes.title": "Scenes",
  "scenes.intro": "Switch the DPI of every mouse and the lighting of every lit device by the program in front.",
  "scenes.more": "Order: custom scenes (by program) → Game → Desktop. Anything on \"Keep current\" is left alone.",
  "scenes.add": "+ New scene",
  "scenes.newName": "Scene {n}",
  "scenes.delete": "Delete scene",
  "scenes.game": "Game",
  "scenes.gameHint": "Used when a game is detected.",
  "scenes.gameMore": "Detected: fullscreen or borderless fullscreen with GPU 3D at least 15%. Add games it misses, such as windowed ones, under Programs.",
  "scenes.desktop": "Desktop",
  "scenes.desktopHint": "Used when nothing else matches.",
  "scenes.desktopMore": "It also restores your settings when you leave another scene; setting lighting here keeps all devices in sync.",
  "scenes.programs": "Programs",
  "scenes.appsCustom": "Program file names, comma-separated, e.g. obs64.exe, code.exe",
  "scenes.appsGame": "More programs, e.g. windowed-game.exe (optional)",
  "scenes.dpi": "DPI (mice)",
  "scenes.effect": "Effect (lit devices)",
  "scenes.keep": "Keep current",
  "scenes.noPrograms": "No programs",
  "scenes.nothingSet": "Keeps everything as is",
  "scenes.edit": "Expand to edit",
  "scenes.brightnessToggle": "Check to let the scene set brightness",

  "settings.title": "Settings",
  "settings.language": "Language",
  "settings.languageHint": "Language of the window and the tray menu.",
  "settings.langSystem": "System default",
  "settings.autostart": "Start at login",
  "settings.autostartHint": "Starts in the tray at login and runs lightweight in the background.",
  "settings.autostartOn": "Start at login turned on",
  "settings.autostartOff": "Start at login turned off",
  "settings.notify": "Low-battery notification",
  "settings.notifyHint": "Show one Windows notification when a wireless device runs low.",
  "settings.notifyMore": "Triggered at the device's own warning threshold (20% if it has none). Hover the tray icon to see each device's battery.",
  "settings.notifyOn": "Low-battery notifications on",
  "settings.notifyOff": "Low-battery notifications off",
  "settings.about": "About",
  "settings.aboutText":
    "OpenSynapse is an open-source, lightweight settings app for Razer peripherals. It is not affiliated with Razer Inc.<br />Closing the window leaves it in the tray; right-click the tray icon to quit. Do not run it together with Razer Synapse, or the two will overwrite each other's settings.<br />Protocol details come from the public documentation of the openrazer project.",
};

const dicts: Record<string, Record<Key, string>> = { "zh-CN": zh, en };
let cur: Record<Key, string> = zh;

export function setLang(lang: string) {
  cur = dicts[lang] ?? en;
  document.documentElement.lang = lang;
}

export function t(key: Key, params?: Record<string, string | number>) {
  const s = cur[key];
  return params ? s.replace(/\{(\w+)\}/g, (_, k) => String(params[k] ?? "")) : s;
}

// translateDom fills [data-i18n] text, [data-i18n-html], [data-i18n-title] and
// [data-i18n-placeholder] from the current language.
export function translateDom(root: ParentNode = document) {
  root.querySelectorAll<HTMLElement>("[data-i18n]").forEach((el) => (el.textContent = t(el.dataset.i18n as Key)));
  root.querySelectorAll<HTMLElement>("[data-i18n-html]").forEach((el) => (el.innerHTML = t(el.dataset.i18nHtml as Key)));
  root.querySelectorAll<HTMLElement>("[data-i18n-title]").forEach((el) => (el.title = t(el.dataset.i18nTitle as Key)));
  root
    .querySelectorAll<HTMLInputElement>("[data-i18n-placeholder]")
    .forEach((el) => (el.placeholder = t(el.dataset.i18nPlaceholder as Key)));
}

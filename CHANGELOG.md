# Changelog

Each release has a section here; the app shows it when it offers the update.
MyGo's update window shows a section as it is, in one language for everyone,
so from 0.3.1 each section is written in Simplified Chinese, English and Japanese.

## 0.5.0

### 简体中文

- 音频: 支持 WalkPlay 方案的 USB 小尾巴声卡 (如 EPZ TP35 Pro)，可调 10 段参量均衡器、数字滤波器、高增益、功放模式和麦克风增益
- 均衡器预设: 内置低音增强、人声、FPS 游戏等预设，也能保存自己的预设，备份时一并导出
- 声音输出: 在设置中直接调整每个播放设备的位深度和采样率

### English

- Audio: tune USB audio dongles built on WalkPlay chips, such as the EPZ TP35 Pro, with a 10-band parametric EQ, the digital filter, high gain, the amplifier mode and the microphone gain
- EQ presets: start from built-in presets such as bass boost, vocal and FPS games, or save your own; they come along when you back up your settings
- Sound output: set the bit depth and sample rate of every playback device right from Settings

### 日本語

- オーディオ: EPZ TP35 Pro など WalkPlay チップを搭載した USB オーディオ ドングルで、10 バンドのパラメトリック EQ、デジタル フィルター、ハイ ゲイン、アンプ モード、マイク ゲインを調整できるようになりました
- EQ プリセット: 低音強調、ボーカル、FPS ゲームなどの組み込みプリセットを選べるほか、自分のプリセットも保存できます。設定のバックアップにも含まれます
- サウンド出力: 各再生デバイスのビット深度とサンプル レートを [設定] から直接変更できるようになりました

## 0.4.1

### 简体中文

- 稳定性: 设置文件异常时不再卡在启动，文件也不会被覆盖，你的设置得以保留
- 宏: 快速切换或新建宏时，每一处修改都会可靠保存；退出或导入设置后，按键都能恢复原有功能
- 备份: 导入不兼容的设置文件时会直接提示，现有设置保持不变
- 细节: 切换语言后托盘菜单立即更新，关于页排版更整齐，调节 DPI 和亮度只作用于当前设备

### English

- Reliability: a damaged settings file no longer stops OpenSynapse from starting, and it's kept instead of overwritten
- Macros: every edit is saved, even when you switch or create macros quickly, and buttons always get their own function back when you quit or import settings
- Backup: importing an incompatible settings file now tells you so and leaves your current settings untouched
- Polish: the tray menu follows a language change right away, the About page reads more neatly, and DPI and brightness changes stay on the device you're adjusting

### 日本語

- 安定性: 設定ファイルに問題があっても起動できなくなることはなくなり、そのファイルも上書きされずに残ります
- マクロ: マクロをすばやく切り替えたり作成したりしても、すべての変更が保存されます。終了時や設定のインポート後は、ボタンが本来の機能に確実に戻ります
- バックアップ: 互換性のない設定ファイルをインポートすると、現在の設定を変えずにその旨を表示します
- 細部: 言語を切り替えるとトレイ メニューにすぐ反映され、[情報] ページが見やすくなり、DPI と明るさの変更は調整中のデバイスにだけ適用されます

## 0.4.0

### 简体中文

- Hypershift: 按住设为 Hypershift 的按键，其他按键即可切换到第二套功能，一键多用
- 宏: 支持录制和编辑鼠标移动与滚轮操作，包括水平滚轮
- 界面: 各页面布局更加整齐统一，情景模式用语更清晰

### English

- Hypershift: hold a Hypershift button to give your other buttons a second set of functions
- Macros: record and edit mouse movement and scrolling, including the horizontal wheel
- Interface: a cleaner, more consistent layout on every page

### 日本語

- Hypershift: Hypershift に設定したボタンを押している間、ほかのボタンで 2 つ目の機能を使えるようになりました
- マクロ: マウスの移動とスクロール (水平スクロールを含む) を記録、編集できるようになりました
- 画面: すべてのページのレイアウトを整え、より見やすくなりました

## 0.3.1

### 简体中文

- DPI 切换提示的圆角和边框不再被裁切
- 设备信息页的“查找配对工具”会在 Razer 支持站搜索该型号的配对工具
- 从此版本起，更新日志提供简体中文、英文和日文

### English

- The DPI display's corners and border are no longer cut off
- Find a pairing utility on the Info tab searches Razer support for the model's pairing utility
- From this version on, release notes come in Simplified Chinese, English and Japanese

### 日本語

- DPI の表示の角と枠線が欠けなくなりました
- [情報] タブの [ペアリング ユーティリティを探す] で、Razer サポートからモデルのペアリング ユーティリティを検索します
- このバージョンから、変更履歴を簡体字中国語、英語、日本語で提供します

## 0.3.0

- Buttons: turn any button but the left one into another mouse button, a DPI action or a key or key combination, stored in the mouse
- Lift-off distance: low, medium or high, or separate lift-off and landing distances
- Info tab: firmware version and serial number, with links to Razer's firmware updater and pairing utilities
- DPI display: the new DPI shows on the screen when you press the mouse's DPI button
- Back up settings: export and import scenes, macros, button bindings and other settings
- Glass effect: a translucent window on Windows 11 22H2 and later

## 0.2.0

- New About page: the version, software update (check now, last check, automatic checks), and links to GitHub, the changelog, issues and the license
- Settings now holds only language, start at login and notifications
- openrazer in the credits opens its project page

## 0.1.1

- The images are built into the executable: the portable version is now just OpenSynapse.exe

## 0.1.0

First release.

- DPI stages, polling rate, lighting, power and receiver indicator settings for about 210 Razer devices
- Smart polling rate and scenes that follow the program in front
- Macros on mouse buttons, recorded or edited in the app
- Low-battery notifications and a tray menu
- English, Simplified Chinese and Japanese
- Automatic updates for the installed and the portable version

# OpenSynapse

A lightweight, open-source settings app for Razer mice. Built in Go with a [MyGo](https://github.com/egoist/mygo) WebView2 frontend. Not affiliated with Razer Inc.

- **Background cost:** the resident process is Go only (HID, tray, smart polling). It uses about 20–30 MB. The WebView2 window exists only while it is open, and closing it ends every browser process.
- **Supported devices:** about 210 Razer mice, keyboards, mouse mats and accessories (laptops are left out). Each device page only shows the controls that device declares. The device table `internal/razer/data/devices.json` is generated from openrazer by `scripts/gendevices.py`. Only the DeathAdder V4 Pro (`1532:00BE` wired, `1532:00BF` wireless) has been tested on real hardware.

## Features

- Device overview: every connected device appears as a card; click a card to open its settings. Plugging and unplugging are picked up within about 3 s. A wireless mouse on its cable is shown once, by whichever connection answers, and an open device page follows it between cable and receiver. If a device is unplugged, its page goes back to the overview.
- DPI stages (1–5, X/Y independent) or a single DPI value
- Lighting for the whole device: off, static, breathing, spectrum or wave (whichever the device supports), plus color and brightness. Devices cannot report their current effect, so the app remembers the last one set here.
- Polling rate, 125–8000 Hz. On wireless mice that use a receiver, both the mouse and the receiver are set.
- Battery, charging state, sleep timeout, low-battery threshold
- **Smart polling rate:** every mouse switches to the game rate while a game is in front, and to the normal rate otherwise. Per-program exceptions are checked first. "最高" means each mouse's own maximum, and a rate above a mouse's maximum uses that maximum. A game is detected as described under scenes (step 2 below).
- **Scenes:** device settings change with the program in the foreground. Each scene can set the mouse DPI, plus the effect, color and brightness of every lit device. Anything left on 「保持当前」 is not touched. Scenes are matched in this order:
  1. The first custom scene that lists the program (e.g. `obs64.exe`).
  2. The built-in game scene: any program auto-detected as a game, plus programs you list for it (e.g. games that run windowed). A game is a fullscreen or borderless-fullscreen window whose process uses at least 15% of the GPU 3D engine. Once recognized, a process stays a game until it exits. Browsers and video players are excluded. GPU load is measured only while an unrecognized fullscreen window is in front.
  3. Otherwise the Desktop scene. It also restores your settings when you leave another scene, and putting lighting in it keeps all your devices in sync.

  The program must stay in front for about 2 s before a switch. Each device is written once per switch, and a device that does not answer is retried every 10 s. A switch overrides settings you changed by hand. The old smart polling-rate setup is migrated automatically. Turn either feature on in the 情景 page or from the tray right-click menu.
- **Low-battery notification:** a wireless device that drops to its warning threshold (20% if it has none) triggers one Windows notification, which is re-armed once the device charges. Hover the tray icon to see the active scene and each device's battery.
- HyperPolling receiver indicators: choose what each of the three LEDs shows (off, battery, connection, polling rate, DPI). LED color is not in the public protocol.
- Tray app with optional start at login (`--tray`, HKCU Run)

**Quit Razer Synapse before using this.** Both apps write the same HID settings and will overwrite each other.

## Development

Requirements: Go 1.27.3+ and Bun.

```sh
bun install
bun run dev        # live reload
bun run build      # build/windows-amd64/OpenSynapse.exe + installer
go test ./...
go run ./cmd/probe # print device state, read-only

# regenerate the device table from an openrazer checkout
uv run --no-project python scripts/gendevices.py ../openrazer
```

## Credits

- Protocol details and the device table come from [openrazer](https://github.com/openrazer/openrazer) (GPL-2.0). The table is generated from its daemon device classes and driver sources; the protocol was reimplemented here and no openrazer code is copied.
- The UI only borrows Synapse's general style (dark theme, green accent). The icons and artwork are original.

# OpenSynapse

**English** · [简体中文](README.zh-CN.md) · [日本語](README.ja.md)

A lightweight, open-source settings app for Razer mice and other Razer devices on Windows. Set DPI, lighting, polling rate and power options, and let your devices adjust themselves to the game or program you're using, without running Razer Synapse.

OpenSynapse is not affiliated with Razer Inc.

## Why OpenSynapse

- **Light on your PC.** In the background, OpenSynapse is a single small process that uses about 20–30 MB of memory. The settings window is loaded only while it's open, and closing it frees all of its memory.
- **No account, no cloud.** Everything stays on your PC. OpenSynapse doesn't connect to the internet.
- **Adapts to what you're doing.** Raise the polling rate when a game starts, switch DPI and lighting for specific programs, and go back to your usual settings afterward.

> [!IMPORTANT]
> Quit Razer Synapse before you use OpenSynapse. Both apps write the same device settings and will overwrite each other.

## Get started

1. Make sure you're on 64-bit Windows 10 or Windows 11 with the [Microsoft Edge WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/) installed (it comes with Windows 11 and up-to-date Windows 10).
2. Run the installer, `opensynapse-setup-<version>-amd64.exe`, or [build it yourself](#build-from-source).
3. Open OpenSynapse. Every connected device appears as a card on the **Devices** page. Select a card to change its settings.

If a device is missing, turn it on, plug in its receiver, quit Razer Synapse, and then select **Scan again**.

Closing the window keeps OpenSynapse running in the notification area (system tray). To quit, right-click the tray icon and select **Quit**.

## What you can do

### Device settings

- **Sensitivity:** up to 5 DPI stages with separate X/Y values, or a single DPI value. The DPI button on the mouse cycles through your stages.
- **Lighting:** off, static, breathing, spectrum or wave (depending on the device), plus color and brightness.
- **Polling rate:** 125–8000 Hz. For wireless mice, the mouse and its receiver are both updated.
- **Power:** battery level, charging state, sleep timeout and low-battery threshold.
- **Receiver indicators:** on HyperPolling receivers, choose what each of the three lights shows: battery, connection, polling rate or DPI.

Each device shows only the settings it supports. Plugging in, unplugging and switching a wireless mouse between cable and receiver are picked up within a few seconds.

### Automatic switching

Go to the **Scenes** page, or right-click the tray icon, to turn these on.

- **Smart polling rate:** your mice use a higher rate while you're in a game and a lower one otherwise, which saves battery and CPU. You can also fix the rate for specific programs. **Highest** means each mouse's own maximum.
- **Scenes:** change mouse DPI and device lighting based on the program in front. Anything set to **Keep current** is left alone. OpenSynapse picks a scene in this order:
  1. **Your own scenes**, matched by program file name (for example, `obs64.exe`).
  2. **Game**, used when a game is detected. Add games it misses, such as windowed games, under **Programs**.
  3. **Desktop**, used for everything else. It also restores your settings when you leave another scene, so lighting set here keeps all your devices in sync.

A program needs to stay in front for about 2 seconds before OpenSynapse switches. Settings you change by hand are replaced at the next switch.

**How games are detected:** a fullscreen or borderless-fullscreen window whose program uses at least 15% of the GPU's 3D engine. Browsers and video players are never treated as games.

### Notifications and tray

- **Low-battery notification:** a Windows notification when a wireless device reaches its warning level (20% if the device has none). You're notified once per discharge.
- Hover over the tray icon to see the active scene and the battery level of each device.
- **Start at login** starts OpenSynapse in the tray when you sign in to Windows.
- The app is available in English and Chinese, and follows your Windows language by default. Change it under **Settings** > **Language**.

## Supported devices

OpenSynapse knows about 210 Razer mice, keyboards, mouse mats and accessories (laptops aren't supported). The device list comes from the [openrazer](https://github.com/openrazer/openrazer) project.

So far, only the **DeathAdder V4 Pro** has been tested on real hardware. Other devices should work but haven't been verified. If you try one, please report how it went.

## FAQ

**Why doesn't the app show the lighting effect my device is using?**
Razer devices can't report their current effect, so OpenSynapse shows the last effect you set in OpenSynapse.

**Can I change the color of the receiver indicators?**
No. The color is set by the firmware and the public protocol can't change it.

**My settings changed by themselves.**
If Smart polling rate or Scenes is on, OpenSynapse changes settings when you switch programs. Turn them off on the **Scenes** page or from the tray menu. Also make sure Razer Synapse isn't running.

**Where are my settings stored?**
In `%APPDATA%\OpenSynapse\config.json`.

## Build from source

Requirements: Go 1.27.3 or later, and [Bun](https://bun.sh).

```sh
bun install
bun run dev        # run with live reload
bun run build      # build/windows-amd64/OpenSynapse.exe and the installer
go test ./...
go run ./cmd/probe # print device state (read-only)

# regenerate the device list from an openrazer checkout
uv run --no-project python scripts/gendevices.py ../openrazer
```

The app is written in Go, with a [MyGo](https://github.com/egoist/mygo) WebView2 window. The device list in `internal/razer/data/devices.json` is generated from openrazer by `scripts/gendevices.py`.

## Credits

- Protocol details and the device list come from [openrazer](https://github.com/openrazer/openrazer) (GPL-2.0). The protocol was reimplemented here; no openrazer code is copied.
- The interface borrows only the general look of Razer Synapse (dark theme, green accent). All icons and artwork are original.

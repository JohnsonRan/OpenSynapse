# OpenSynapse

A lightweight, open-source settings app for Razer mice. Built in Go with a [MyGo](https://github.com/egoist/mygo) WebView2 frontend. Not affiliated with Razer Inc.

- **Background cost:** the resident process is Go only (HID, tray, smart polling). It uses about 20–30 MB. The WebView2 window exists only while it is open, and closing it ends every browser process.
- **Supported device:** Razer DeathAdder V4 Pro (`1532:00BE` wired, `1532:00BF` HyperPolling wireless). Only `00BF` has been tested on real hardware. To add a device that uses the same command set, add a row to `Models` in `internal/razer/device.go`.

## Features

- DPI stages (1–5, X/Y independent) and the current DPI
- Polling rate, 125–8000 Hz. On wireless, both the mouse and the receiver are set.
- Battery, charging state, sleep timeout, low-battery threshold
- **Smart polling rate:** picks a rate for whatever program is in the foreground, checked in this order:
  1. Manual rules (e.g. `cs2.exe` → 8000 Hz). They also work as exclusions.
  2. Auto-detection: a fullscreen or borderless-fullscreen window whose process uses ≥ 15% of the GPU 3D engine counts as a game. Once recognized, a process stays a game until it exits. Browsers and video players are excluded. GPU load is measured only while an unrecognized fullscreen window is in front.
  3. Otherwise the default rate (e.g. 1000 Hz), which saves CPU and wireless battery.

  The program must stay in front for about 2 s before the rate changes, and the device is written only when the target rate changes. A smart switch overrides any rate you set by hand. Off by default; turn it on from the Smart page or the tray right-click menu.
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
```

## Credits

- Protocol details come from the public documentation in [openrazer](https://github.com/openrazer/openrazer). They were reimplemented here; no openrazer code is copied.
- The UI only borrows Synapse's general style (dark theme, green accent). The icons and artwork are original.

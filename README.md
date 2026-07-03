# FluentMacWallpaper

macOS menu bar app that loops a local video (`.mp4`, `.mov`, `.m4v`) as your desktop wallpaper.

- Runs as a menu bar item — no dock icon
- Borderless fullscreen window at the native desktop window level: sits **behind** desktop icons, clicks pass through
- Hardware-accelerated playback via AVFoundation (`AVQueuePlayer` + `AVPlayerLooper` for gapless looping) — low CPU and battery usage
- Menu: **Select Video…**, **Pause/Play**, **Quit**

<img src="demo.gif" width="600px" alt="Demo GIF" />

<sup><a href="#wallpaper-sources">1</a></sup> Wallpapers Source

## Disclaimer

I created this application for personal use, and I am not a macOS developer. It is provided as-is, without warranty of any kind. Use at your own risk and if you want to contribute, feel free to submit a pull request.

## Requirements

- macOS (Apple Silicon or Intel)

## Install

### Download the DMG (recommended)

Grab `FluentMacWallpaper-arm64.dmg` (Apple Silicon) or `FluentMacWallpaper-intel.dmg` (Intel) from the
[latest release](https://github.com/StevenCyb/FluentMacWallpaper/releases/latest), open it, and drag
`FluentMacWallpaper.app` into `Applications`.

> **No Apple Developer account.** The app is only ad-hoc signed (no paid Apple Developer certificate,
> no notarization), so Gatekeeper blocks the first launch. To open it:
> - Right-click (or Control-click) `FluentMacWallpaper.app` → **Open** → confirm **Open** in the dialog, or
> - Run `xattr -d com.apple.quarantine /Applications/FluentMacWallpaper.app` in Terminal.
>
> You only need to do this once.

### Build from source

Requires Go 1.22+ and Xcode Command Line Tools (`xcode-select --install`).

```sh
git clone https://github.com/StevenCyb/FluentMacWallpaper.git
cd FluentMacWallpaper
go install .
```

This puts the `FluentMacWallpaper` binary into `$(go env GOPATH)/bin` (make sure it is on your `PATH`).

> **Note:** `go install github.com/StevenCyb/FluentMacWallpaper@latest` does not work, because
> `go.mod` contains a `replace` directive pointing [progrium/darwinkit](https://github.com/progrium/darwinkit)
> to a [purego-based fork](https://github.com/gysddn/darwinkit/tree/purego). Upstream darwinkit
> crashes with `SIGABRT` in its libffi bridge on Go ≥ 1.25
> ([darwinkit#286](https://github.com/progrium/darwinkit/issues/286)); the replace can be dropped
> once [darwinkit#276](https://github.com/progrium/darwinkit/pull/276) is merged and released.

## Usage

```sh
FluentMacWallpaper                  # start, then pick a video via the tray icon
FluentMacWallpaper ~/Movies/bg.mp4  # start and play this video immediately
```

A tray icon appears in the menu bar:

| Menu item | Action |
|---|---|
| Select Video… | Open a file picker and start looping the chosen video |
| Pause / Play | Toggle playback |
| Launch at Login | Toggle starting the app automatically on login (via a LaunchAgent) |
| Quit | Exit and restore your normal wallpaper |

Video is muted and scaled to fill the screen (aspect-fill).

## Wallpaper sources
Video sources used in the demo:

1. Midnight Fuel Stop. MotionBGS. https://motionbgs.com/midnight-fuel-stop
3. Frostfang Barioth Armor (MHW Iceborne). MotionBGS. https://motionbgs.com/frostfang-barioth-armor-mhw-iceborne
4. Morning Serenity. MotionBGS. https://motionbgs.com/morning-serenity

# Anki Passive Widget

A lightweight, native Windows desktop flashcard widget for Anki. It periodically surfaces due or new cards in the bottom-right corner of your screen so you can review passively without keeping the Anki window in focus.

Built in Go directly on Win32 (`user32`, `gdi32`, `shell32`, `dwmapi`, `winmm`) without WebView2 or Chromium, keeping idle memory usage low while supporting Japanese furigana, inline images, audio playback, and both Light and Dark themes.

## Features

- **Native Win32 UI (No WebView)**: Custom anti-aliased pixel canvas with ClearType text, Windows 11 rounded corners, per-monitor DPI scaling, and embedded Lucide SVG icons (`internal/ui/assets/icons/`).
- **Hybrid Data Source**:
  - **Local SQLite (`collection.anki2`)**: Reads and records reviews directly when Anki Desktop is closed (`usn = -1`, `revlog` entries, `col.mod`, and deck studied counters so AnkiWeb syncs cleanly on next open).
  - **AnkiConnect (`http://127.0.0.1:8765`)**: Automatically used when Anki Desktop is running.
- **Per-Deck Field Selection & Deck Tree**: Search and select nested `::` decks, and choose which note fields appear on Front and Back per deck (or revert to Anki template defaults via **Use Template**).
- **Rich Card Rendering**:
  - Preserves line breaks (`<br/>`) and `<ruby>` / `<rt>` Japanese furigana above Kanji.
  - Displays inline images (`PNG`, `JPEG`, `GIF`, `WebP`, `BMP`) from `collection.media` with click-to-zoom (`1/4` screen overlay).
  - Plays card audio from `collection.media` (or via AnkiConnect fallback).
- **Light, Dark, and System Themes**: Follows Windows light/dark theme (`AppsUseLightTheme`) automatically or switches manually via Settings or the header toggle button.
- **System Tray & Focus Protection**: Single-instance lock, system tray menu (`Show Card`, `Pause Timer`, `Settings`, `Quit`), 20-second auto-hide on Front (pauses while hovering or viewing zoomed images), and scheduler protection so background timers never overwrite an open card.

## Usage

### Running the Portable App

1. Download and extract `anki-passive-widget-windows-amd64.zip` from Releases (or build locally).
2. Run `anki-passive-widget.exe`.
3. Right-click the tray icon and open **Settings** to pick your Anki profile, target deck, display fields, interval (`1–60 min`), and theme (`System`, `Light`, `Dark`).

Configuration is stored in `%USERPROFILE%\.anki-passive-widget\config.json`.

### Keyboard Shortcuts

- `Space` or `Enter`: Show Answer (when viewing Front)
- `1`, `2`, `3`, `4`: Grade card (`Again`, `Hard`, `Good`, `Easy`) after revealing
- `R`: Replay card audio
- `Esc`: Close zoomed image / close Settings / Snooze card for 5 minutes

## Building from Source (Windows)

### Prerequisites

- Windows 10 or Windows 11 (x64)
- Go `1.26+`
- `rsrc` for embedding the application icon:

```powershell
go install github.com/akavel/rsrc@latest
```

### Build Executable

```powershell
.\scripts\build-windows.bat
```

This generates `cmd\widget\rsrc.syso` from `build\windows\icon.ico` and builds `anki-passive-widget.exe` with `-ldflags "-w -s -H windowsgui"` from `./cmd/widget`.

### Package Release Archive

```powershell
.\scripts\package-windows.bat
```

This builds `anki-passive-widget.exe` and creates a portable zip archive at `dist\anki-passive-widget-windows-amd64.zip`.

### Running Tests

```powershell
go vet ./...
go test ./...
```

Note: Direct local SQLite write/profile tests require Anki Desktop to be closed because Anki locks `collection.anki2`.

## Project Structure

```text
.
├── .gitignore
├── README.md
├── go.mod
├── go.sum
├── build/
│   ├── appicon.png
│   └── windows/icon.ico
├── cmd/widget/
│   ├── main.go                 # Single-instance entry point and GC/memory configuration
│   └── main_other.go
├── internal/
│   ├── anki/                   # AnkiConnect client, SQLite store, HTML/template parser
│   ├── app/                    # Controller, scheduler, HTML node parser, deck tree, media helpers
│   ├── config/                 # User configuration load/save and profile discovery
│   └── ui/
│       ├── assets/icons/       # Embedded Lucide SVG icons
│       ├── canvas_dib.go       # Anti-aliased DIB canvas and toggle/slider drawing
│       ├── icons.go            # SVG path parser and 4x4 SSAA icon rasterizer
│       ├── theme.go            # Light/Dark palettes and Windows theme registry lookup
│       ├── tray_windows.go     # Native shell32 system tray icon and popup menu
│       ├── win32_api.go        # Win32 user32/gdi32/dwmapi/kernel32 bindings
│       ├── win32_draw.go       # Review card layout, image zoom, and GDI text/image blit
│       ├── win32_settings.go   # Settings view and searchable deck picker overlay
│       └── win32_window.go     # Win32 message loop, DPI scaling, shortcuts, and mouse input
└── scripts/
    ├── build-windows.bat       # Embed icon resource and build Windows GUI binary
    └── package-windows.bat     # Build and package portable zip archive in dist/
```

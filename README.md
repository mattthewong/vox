# Vox

System-wide speech-to-text for macOS. Hold a hotkey, speak, release -- transcribed text appears wherever your cursor is. Runs entirely locally using [whisper.cpp](https://github.com/ggerganov/whisper.cpp). No paid services, no rate limits.

<img src="assets/vox-setup.gif" alt="vox setup">

## How it works

```
Hold hotkey --> Record mic --> Whisper transcribes --> Text pasted at cursor
```

1. Run `make start` -- vox appears in your menubar. The terminal can be closed.
2. Switch to any app -- editor, browser, terminal, chat.
3. Hold your hotkey (e.g. Option+Space, Cmd+Shift), speak naturally.
4. Release -- text appears where your cursor is.

The menubar icon changes to reflect the current state — an outlined waveform when idle, a filled waveform while recording, and a circular arrow while transcribing. (The icons are SF Symbols; tweak them in `internal/ui/ui_darwin.go`.)

Click the menubar icon for the status, the last transcribed line, your configured hotkey, a "Show Log…" shortcut (opens in Console.app), and a Quit Vox menu item. The CLI remains available for `vox setup` and direct invocation if you prefer.

You also hear a gentle chime on start and stop.

## Install

### Quick start (one command)

Requirements:
- **macOS**
- **Homebrew** (https://brew.sh)
- **Go 1.24+**

```bash
git clone https://github.com/mattthewong/vox.git
cd vox
make start
```

`make start` is the only command you need. It will:

1. Install missing system deps (`sox`, `whisper-cpp`) via Homebrew.
2. Download the default Whisper model (~150MB) into `~/.local/share/whisper-cpp/` if missing.
3. Build `bin/Vox.app` and ad-hoc codesign it.
4. Launch `Vox.app` detached. Vox manages `whisper-server` itself when using the default local URL.

The first launch will trigger two macOS permission prompts (Microphone and Accessibility); grant both and you're done. Re-running `make start` after the first time is a near-instant rebuild + launch, since `setup` is idempotent.

### Manual setup (advanced)

```bash
brew install sox whisper-cpp
mkdir -p ~/.local/share/whisper-cpp
curl -L -o ~/.local/share/whisper-cpp/ggml-base.en.bin \
  "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin"
```

### Build or install binary

```bash
make build      # outputs bin/vox (bare binary)
make app        # outputs bin/Vox.app (real macOS bundle, ad-hoc signed)
make install    # installs bin/vox to /usr/local/bin/vox
```

### Start manually

```bash
whisper-server --host 127.0.0.1 --port 2022 \
  --model ~/.local/share/whisper-cpp/ggml-base.en.bin
vox
```

### Lifecycle commands

```bash
make start    # ensures deps, builds Vox.app, launches whisper-server + Vox detached
make stop     # stops Vox (its managed whisper child exits with it)
make status   # shows whether Vox is running
tail -f logs/vox.log
```

`make start` runs `setup` (idempotent install of deps + default base model) and `app` (builds and signs `bin/Vox.app`), then launches `Vox.app/Contents/MacOS/vox` detached via `nohup`. Vox spawns and manages its own `whisper-server` child on `127.0.0.1:2022` -- there's no remote-server mode. PID lands in `logs/vox.pid`, output redirects to `logs/*.log`, and the command returns immediately -- you can close the terminal and Vox keeps running in your menubar. To shut it down either click the menubar icon and choose **Quit Vox**, or run `make stop`.

## macOS permissions

On first run, macOS will prompt for two permissions. Grant them to **Vox** (the entry will appear as `Vox.app` in the System Settings list):

- **Microphone** -- System Settings > Privacy & Security > Microphone
- **Accessibility** -- System Settings > Privacy & Security > Accessibility

Because vox runs inside a `.app` bundle with a stable `CFBundleIdentifier` (`dev.vox.menubar`), the System Settings entry survives rebuilds. The first time you run `make start` you grant the two permissions to `Vox.app`; on subsequent rebuilds the entry is still there. If macOS does still prompt after a rebuild (ad-hoc signed builds get a fresh cdhash), you can usually just toggle the existing entry's checkbox off and back on instead of removing and re-adding the binary.

## Configuration

All via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `VOX_HOTKEY` | `option+space` | Hotkey to trigger recording. Comma-separated for multiple. |
| `VOX_WHISPER_MODEL_ID` | `base.en` | Initial model ID (`tiny.en`, `base.en`, `small.en`, `medium.en`, `large-v3-turbo`) |
| `VOX_HOLD_TO_TALK` | `true` | `true` = hold to record, `false` = toggle on/off |
| `VOX_LANGUAGE` | *(auto-detect)* | BCP-47 language code (e.g. `en`, `es`) |
| `VOX_VERBOSE` | `false` | Debug logging |
| `VOX_LOG_PATH` | *(unset)* | If set, the file at this path is deleted on clean shutdown. `make start` points this at `logs/vox.log`. |
| `VOX_PID_PATH` | *(unset)* | If set, the file at this path is deleted on clean shutdown. `make start` points this at `logs/vox.pid`. |

Toggles set via the menubar (Mode, Play sounds, Auto-paste, Change Hotkey, Whisper Model) are persisted to `~/Library/Application Support/Vox/preferences.json`. Env vars take precedence over the preferences file, which takes precedence over compiled-in defaults.

### Hotkey formats

```bash
VOX_HOTKEY="fn"                 # Fn / Globe key
VOX_HOTKEY="cmd+shift"          # Modifier-only (no extra key needed)
VOX_HOTKEY="option+space"       # Modifier + key
VOX_HOTKEY="ctrl+shift+d"       # Multiple modifiers + key
VOX_HOTKEY="fn,cmd+shift"       # Multiple hotkeys (either triggers)
```

**Available modifiers:** `ctrl`, `shift`, `option`/`alt`, `cmd`/`command`
**Available keys:** `a-z`, `0-9`, `f1-f20`, `space`, `return`, `escape`, `tab`, `delete`, arrow keys

## Architecture

```
cmd/vox/main.go          -- Entrypoint, event loops, signal/menubar shutdown wiring
internal/hotkey/          -- CGEventTap-based global hotkey (modifier-only, fn, modifier+key)
  hotkey_darwin.go        -- Go listener with keydown/keyup channels
  bridge.c                -- C event tap callback (registers on main run loop, non-blocking)
internal/audio/           -- Mic recording via ffmpeg/sox subprocess
  recorder.go             -- Start/stop recording, WAV output
  sound.go                -- Embedded chime sounds (start/stop)
internal/transcribe/      -- Whisper HTTP client
  client.go               -- Multipart upload, auto-detects /inference vs /v1/audio/transcriptions
internal/inject/          -- Text injection into focused app
  paste_darwin.go         -- pbcopy + CGEvent Cmd+V (works in any app)
internal/pipeline/        -- Generic stage pipeline applied after transcription
internal/ui/              -- Menubar status item (Cocoa via cgo)
  ui_darwin.go / .m       -- NSStatusItem, NSApp run loop
internal/config/          -- Env var config + hotkey string parsing
packaging/Info.plist      -- macOS bundle metadata (CFBundleIdentifier, LSUIElement, NSMicrophoneUsageDescription)
```

Threading on macOS: the main goroutine owns NSApp's run loop (`ui.Run()`). The CGEventTap registers its run loop source on that same main loop, so menubar clicks and hotkey events are dispatched on the same thread. All other work (event handling, recording, transcription, paste) happens in goroutines.

`make app` wraps the binary in a real `.app` bundle and ad-hoc signs it with a stable identifier (`dev.vox.menubar`). This is what makes macOS treat the rebuilt binary as the same app for TCC (Microphone, Accessibility) trust purposes — without it, every rebuild gets a fresh cdhash and the System Settings entries effectively reset.

## Development

```bash
make build        # Build bare binary (bin/vox)
make app          # Wrap into bin/Vox.app (.app bundle, ad-hoc codesigned)
make test         # Run all tests
make test-short   # Skip integration tests
make lint         # go vet
make fmt          # gofmt
make run          # Build and run
```

## Why

I was using Whisper Flow for speech-to-text but kept hitting rate limits on their free plan. Vox does the same thing -- system-wide dictation with a hold-to-talk hotkey -- but runs entirely on your machine with no external dependencies.

## License

MIT

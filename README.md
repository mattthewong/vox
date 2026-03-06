# Vox

System-wide speech-to-text for macOS. Hold a hotkey, speak, release -- transcribed text appears wherever your cursor is. Runs entirely locally using [whisper.cpp](https://github.com/ggerganov/whisper.cpp). No paid services, no rate limits.

## How it works

```
Hold hotkey --> Record mic --> Whisper transcribes --> Text pasted at cursor
```

1. You run `vox` in a terminal (or as a background process)
2. Switch to any app -- editor, browser, terminal, chat
3. Hold your hotkey (e.g. Fn, Cmd+Shift), speak naturally
4. Release -- text appears where your cursor is

You hear a gentle chime on start and stop.

## Install

### Prerequisites

- **Go 1.24+**
- **ffmpeg** or **sox** for audio recording (`brew install ffmpeg` or `brew install sox`)
- **whisper.cpp** for local transcription (`brew install whisper-cpp`)
- A Whisper model file:
  ```bash
  mkdir -p ~/.local/share/whisper-cpp
  curl -L -o ~/.local/share/whisper-cpp/ggml-base.en.bin \
    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin"
  ```

### Build

```bash
git clone https://github.com/mattthewong/vox.git
cd vox
make build      # outputs bin/vox
make install    # installs to /usr/local/bin/vox
```

### Start Whisper server

```bash
whisper-server --host 127.0.0.1 --port 2022 \
  --model ~/.local/share/whisper-cpp/ggml-base.en.bin
```

### Run

```bash
vox
# or with custom hotkey:
VOX_HOTKEY="fn,cmd+shift" vox
```

## macOS permissions

On first run, macOS will prompt for two permissions. Grant them to your terminal app (Terminal, iTerm2, Ghostty, etc.):

- **Microphone** -- System Settings > Privacy & Security > Microphone
- **Accessibility** -- System Settings > Privacy & Security > Accessibility

## Configuration

All via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `VOX_HOTKEY` | `option+space` | Hotkey to trigger recording. Comma-separated for multiple. |
| `WHISPER_URL` | `http://127.0.0.1:2022` | Whisper server URL |
| `VOX_HOLD_TO_TALK` | `true` | `true` = hold to record, `false` = toggle on/off |
| `VOX_LANGUAGE` | *(auto-detect)* | BCP-47 language code (e.g. `en`, `es`) |
| `VOX_VERBOSE` | `false` | Debug logging |

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
cmd/vox/main.go          -- Entrypoint, hotkey event loop, orchestration
internal/hotkey/          -- CGEventTap-based global hotkey (supports modifier-only, fn, modifier+key)
  hotkey_darwin.go        -- Go listener with keydown/keyup channels
  bridge.c                -- C event tap callback
internal/audio/           -- Mic recording via ffmpeg/sox subprocess
  recorder.go             -- Start/stop recording, WAV output
  sound.go                -- Embedded chime sounds (start/stop)
internal/transcribe/      -- Whisper HTTP client
  client.go               -- Multipart upload, auto-detects /inference vs /v1/audio/transcriptions
internal/inject/          -- Text injection into focused app
  paste_darwin.go         -- pbcopy + CGEvent Cmd+V (works in any app)
internal/config/          -- Env var config + hotkey string parsing
```

## Development

```bash
make build        # Build binary
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

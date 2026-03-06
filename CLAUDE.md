# vox

System-wide speech-to-text dictation tool for macOS. Press a global hotkey, speak, and the transcribed text is pasted into the focused application.

## Architecture

```
Global Hotkey (Option+Space) → Record Audio (sox/ffmpeg) → Whisper Server → Paste Text (System Events)
```

- **Hotkey**: `golang.design/x/hotkey` registers a system-wide Option+Space shortcut. Hold-to-talk (default) or toggle mode.
- **Audio**: Records via `rec` (sox) or `ffmpeg` to a temp WAV file (16kHz, mono, 16-bit).
- **Transcribe**: Sends WAV to a local Whisper HTTP server for speech-to-text.
- **Inject**: Copies text to clipboard via `pbcopy`, then simulates Cmd+V via `osascript` / System Events.

## Build & Run

```bash
make deps      # brew install sox (if needed)
make build     # outputs bin/vox
make run       # go run ./cmd/vox
make install   # installs to /usr/local/bin/vox
make test      # go test -v ./...
make lint      # go vet + staticcheck
make fmt       # gofmt
```

## Dependencies

- Go 1.24+
- sox (`brew install sox`) or ffmpeg for audio recording
- Local Whisper server on http://127.0.0.1:2022 (configurable)

## Configuration

All via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `WHISPER_URL` | `http://127.0.0.1:2022` | Whisper transcription server URL |
| `VOX_LANGUAGE` | (auto-detect) | BCP-47 language code (e.g. `en`, `es`) |
| `VOX_HOLD_TO_TALK` | `true` | `true` = hold hotkey to record; `false` = toggle on/off |
| `VOX_VERBOSE` | `false` | Enable debug logging |

## Default Hotkey

**Option+Space** (hold to talk). Release to stop recording and transcribe.

## Key Files

| Path | Purpose |
|------|---------|
| `cmd/vox/main.go` | CLI entrypoint, hotkey event loop, orchestration |
| `internal/audio/recorder.go` | Microphone recording via sox/ffmpeg subprocess |
| `internal/transcribe/` | Whisper HTTP client (health check + transcription) |
| `internal/inject/paste_darwin.go` | Clipboard + paste via pbcopy/osascript |
| `internal/config/config.go` | Environment variable config loading |

## macOS Permissions Required

- **Microphone**: System Preferences → Privacy & Security → Microphone → allow Terminal / your terminal app
- **Accessibility**: System Preferences → Privacy & Security → Accessibility → allow Terminal (needed for System Events keystroke injection)

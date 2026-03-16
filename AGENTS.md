# Vox -- Codex Reference

## What This Is

System-wide speech-to-text for macOS. Hold a global hotkey, speak, release -- text is pasted into the focused app. Uses whisper.cpp locally for transcription. No cloud services, no rate limits.

## Architecture

```
CGEventTap (hotkey) --> ffmpeg/sox (record) --> whisper.cpp HTTP (transcribe) --> CGEvent Cmd+V (paste)
```

- **Hotkey**: Custom CGEventTap monitors `kCGEventFlagsChanged` + `kCGEventKeyDown/Up`. Supports modifier-only (cmd+shift), fn key, and modifier+key (option+space) triggers.
- **Audio**: Records via `ffmpeg -f avfoundation` or `sox rec` to temp WAV (16kHz, mono, 16-bit). Auto-detects available tool.
- **Transcribe**: HTTP multipart POST to whisper.cpp server. Auto-detects endpoint (`/inference` for whisper.cpp, `/v1/audio/transcriptions` for OpenAI-compatible).
- **Inject**: `pbcopy` + `CGEventCreateKeyboardEvent` Cmd+V. CGEvent approach works in any focused app (unlike osascript).
- **Sounds**: Embedded WAV chimes via `go:embed`, played with `afplay`.

## Build & Run

```bash
make build     # bin/vox
make run       # go run ./cmd/vox
make install   # /usr/local/bin/vox
make test      # go test -v ./...
make lint      # go vet
make fmt       # gofmt
```

## Dependencies

- Go 1.24+
- ffmpeg or sox (audio recording)
- whisper.cpp server (default: http://127.0.0.1:2022)
- macOS Accessibility + Microphone permissions

## Configuration (env vars)

| Variable | Default | Description |
|----------|---------|-------------|
| `VOX_HOTKEY` | `option+space` | Comma-separated hotkeys: `fn`, `cmd+shift`, `option+space` |
| `WHISPER_URL` | `http://127.0.0.1:2022` | whisper.cpp server URL |
| `VOX_HOLD_TO_TALK` | `true` | Hold to record vs toggle |
| `VOX_LANGUAGE` | *(auto)* | BCP-47 language code |
| `VOX_VERBOSE` | `false` | Debug logging |

## Key Files

| Path | Purpose |
|------|---------|
| `cmd/vox/main.go` | Entrypoint, event loops (hold-to-talk + toggle), signal handling |
| `internal/hotkey/hotkey_darwin.go` | CGEventTap listener, trigger matching, keydown/keyup channels |
| `internal/hotkey/bridge.c` | C event tap callback, accessibility check |
| `internal/audio/recorder.go` | ffmpeg/sox subprocess management, WAV recording |
| `internal/audio/sound.go` | Embedded chime sounds, afplay playback |
| `internal/transcribe/client.go` | whisper.cpp HTTP client, endpoint auto-detection |
| `internal/inject/paste_darwin.go` | CGEvent Cmd+V paste, pbcopy clipboard |
| `internal/config/config.go` | Env config, hotkey string parsing |

## Critical Notes

1. **CGEventTap requires Accessibility permission.** Checks `AXIsProcessTrusted()` at startup.
2. **Paste uses CGEvent, not osascript.** `CGEventCreateKeyboardEvent` posts Cmd+V directly to the system.
3. **Modifier-only hotkey cancellation.** If cmd+shift is held then a regular key is pressed, recording cancels (distinguishes dictation from keyboard shortcuts).
4. **Whisper endpoint auto-detection.** Probes `/v1/audio/transcriptions` first, falls back to `/inference` (whisper.cpp native). Cached after first probe.
5. **Sound playback.** Chimes are `go:embed`ded WAVs written to temp files and played via `afplay`. Cleaned up after playback.

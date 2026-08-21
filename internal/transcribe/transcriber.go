package transcribe

import "context"

// Transcriber converts recorded audio into text. Implementations may call a
// local HTTP server (whisper.cpp), run inference in-process (Parakeet via
// sherpa-onnx), or stub the call out in tests.
//
// wavData is a complete 16 kHz mono 16-bit WAV payload as produced by
// internal/audio.
//
// TranscribeOptions is the seam for engine-specific knobs. Not every option
// applies to every engine: Parakeet ignores InitialPrompt (it has no
// equivalent of whisper's prompt conditioning) and ignores Language, because
// the chosen model fixes the language set. If a future engine needs a runtime
// language hint (Parakeet v3 covers 25 languages but auto-detects), add it
// here rather than widening the interface.
type Transcriber interface {
	Transcribe(ctx context.Context, wavData []byte, opts TranscribeOptions) (string, error)
}

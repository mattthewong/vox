// Package sttmodel is the catalog of speech-to-text models vox can download
// and run. It covers both whisper.cpp GGML models (a single .bin file) and
// Parakeet models (a directory of ONNX files extracted from a tar.bz2).
package sttmodel

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Engine identifies which inference backend runs a model.
type Engine string

const (
	EngineWhisper  Engine = "whisper"
	EngineParakeet Engine = "parakeet"
)

// DefaultID is the model vox uses when no override exists in flags, env, or
// prefs. Whisper stays the default until benchmarks justify switching.
const DefaultID = "base.en"

// Model describes one downloadable STT model.
type Model struct {
	ID       string
	Engine   Engine
	Label    string
	URL      string
	Checksum string // SHA-256 of the downloaded artifact
	SizeMB   int

	// Descriptor is a short phrase shown in the menubar model picker next to
	// the label, summarizing speed/accuracy tradeoff and size. Example:
	// "Fast · Good accuracy · 142 MB". Rendered in a smaller, secondary font.
	Descriptor string

	// Badge is a right-aligned tag for this model's distinguishing trait.
	// At most one model should be "Recommended"; at most one "Default".
	// Multilingual models carry their language scope. Empty for most rows.
	// Rendered as NSMenuItemBadge on macOS 14+.
	Badge string

	// Blurb is tooltip text shown on hover, carrying the evidence behind the
	// descriptor (measured WER, native punctuation, etc).
	Blurb string

	// Filename is set for single-file models (whisper GGML). Empty for archives.
	Filename string

	// Dirname is the extracted directory name for archive models. Empty for
	// single-file models.
	Dirname string

	// Files lists paths (relative to Dirname) that must exist for an archive
	// model to count as installed. Empty for single-file models.
	Files []string
}

// IsArchive reports whether the model ships as an archive that extracts into a
// directory, rather than a single downloadable file.
func (m Model) IsArchive() bool { return m.Dirname != "" }

// Whisper checksums are SHA-256 from HuggingFace Git LFS pointers:
// https://huggingface.co/ggerganov/whisper.cpp/tree/main
//
// Parakeet checksums are SHA-256 of the sherpa-onnx release archives, verified
// by download on 2026-08-19.
var catalog = []Model{
	{
		ID:         "tiny.en",
		Engine:     EngineWhisper,
		Label:      "Whisper Tiny",
		Descriptor: "Fastest · Basic accuracy · 75 MB",
		Badge:      "",
		Blurb:      "~7.7% WER on Open ASR Leaderboard. Lowest latency, lowest accuracy. English only.",
		Filename:   "ggml-tiny.en.bin",
		URL:        "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.en.bin",
		Checksum:   "921e4cf8686fdd993dcd081a5da5b6c365bfde1162e72b08d75ac75289920b1f",
		SizeMB:     75,
	},
	{
		ID:         "base.en",
		Engine:     EngineWhisper,
		Label:      "Whisper Base",
		Descriptor: "Fast · Good accuracy · 142 MB",
		Badge:      "Default",
		Blurb:      "~5.2% WER. Good balance of speed and accuracy for most dictation. English only.",
		Filename:   "ggml-base.en.bin",
		URL:        "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin",
		Checksum:   "a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002",
		SizeMB:     142,
	},
	{
		ID:         "small.en",
		Engine:     EngineWhisper,
		Label:      "Whisper Small",
		Descriptor: "Medium · Better accuracy · 466 MB",
		Badge:      "",
		Blurb:      "~3.9% WER. Noticeably more accurate than Base at 3x the size. English only.",
		Filename:   "ggml-small.en.bin",
		URL:        "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.en.bin",
		Checksum:   "c6138d6d58ecc8322097e0f987c32f1be8bb0a18532a3f88f734d1bbf9c41e5d",
		SizeMB:     466,
	},
	{
		ID:         "medium.en",
		Engine:     EngineWhisper,
		Label:      "Whisper Medium",
		Descriptor: "Slow · High accuracy · 1.5 GB",
		Badge:      "",
		Blurb:      "~3.5% WER. Marginal gain over Small; significantly slower. English only.",
		Filename:   "ggml-medium.en.bin",
		URL:        "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-medium.en.bin",
		Checksum:   "cc37e93478338ec7700281a7ac30a10128929eb8f427dda2e865faa8f6da4356",
		SizeMB:     1536,
	},
	{
		ID:         "large-v3-turbo",
		Engine:     EngineWhisper,
		Label:      "Whisper Large v3 Turbo",
		Descriptor: "Medium · High accuracy · 1.5 GB",
		Badge:      "Multilingual",
		Blurb:      "~3.3% WER. Distilled from Large v3; faster than Medium despite equal size. ~99 languages, auto-detected.",
		Filename:   "ggml-large-v3-turbo.bin",
		URL:        "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3-turbo.bin",
		Checksum:   "1fc70f774d38eb169993ac391eea357ef47c88757ef72ee5943879b7e8e2bc69",
		SizeMB:     1536,
	},
	{
		ID:         "parakeet-v2",
		Engine:     EngineParakeet,
		Label:      "Parakeet v2",
		Descriptor: "Fast · Highest accuracy · 460 MB",
		Badge:      "Recommended",
		Blurb:      "2.06% WER measured on 50 LibriSpeech clips (vs base.en 3.93%). Punctuates and capitalizes natively. English only. ~1s model load after idle.",
		Dirname:    "sherpa-onnx-nemo-parakeet-tdt-0.6b-v2-int8",
		URL:        "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-nemo-parakeet-tdt-0.6b-v2-int8.tar.bz2",
		// Verified 2026-08-19 (482468385 bytes).
		Checksum: "157c157bc51155e03e37d2466522a3a737dd9c72bb25f36eb18912964161e1ad",
		SizeMB:   460,
		Files: []string{
			"encoder.int8.onnx",
			"decoder.int8.onnx",
			"joiner.int8.onnx",
			"tokens.txt",
		},
	},
	{
		ID:         "parakeet-v3",
		Engine:     EngineParakeet,
		Label:      "Parakeet v3",
		Descriptor: "Fast · Highest accuracy · 465 MB",
		Badge:      "25 languages",
		Blurb:      "25 European languages, auto-detected. Similar accuracy to v2. Punctuates and capitalizes natively. ~1s model load after idle.",
		Dirname:    "sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8",
		URL:        "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8.tar.bz2",
		// Verified 2026-08-19 (487170055 bytes).
		Checksum: "5793d0fd397c5778d2cf2126994d58e9d56b1be7c04d13c7a15bb1b4eafb16bf",
		SizeMB:   465,
		Files: []string{
			"encoder.int8.onnx",
			"decoder.int8.onnx",
			"joiner.int8.onnx",
			"tokens.txt",
		},
	},
}

// All returns a copy of the catalog.
func All() []Model { return slices.Clone(catalog) }

// ByID resolves a model by its stable ID.
func ByID(id string) (Model, bool) {
	for _, m := range catalog {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// ByEngine returns every catalog model for one engine, in catalog order.
func ByEngine(e Engine) []Model {
	var out []Model
	for _, m := range catalog {
		if m.Engine == e {
			out = append(out, m)
		}
	}
	return out
}

// RootDir is the base directory for all downloaded models.
// VOX_MODEL_DIR overrides it, primarily for tests.
func RootDir() (string, error) {
	if p := os.Getenv("VOX_MODEL_DIR"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "vox", "models"), nil
}

// legacyWhisperDir is where vox stored GGML models before the sttmodel split.
// Honored so existing installs do not re-download multi-gigabyte models.
func legacyWhisperDir() (string, error) {
	if p := os.Getenv("WHISPER_MODEL_DIR"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "whisper-cpp"), nil
}

// Path is the canonical location for a model: a file path for single-file
// models, a directory path for archive models. New downloads always land here.
func Path(m Model) (string, error) {
	root, err := RootDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, string(m.Engine))
	if m.IsArchive() {
		return filepath.Join(dir, m.Dirname), nil
	}
	return filepath.Join(dir, m.Filename), nil
}

// legacyPath is the pre-migration location, or "" if the model has none.
func legacyPath(m Model) (string, error) {
	if m.Engine != EngineWhisper {
		return "", nil
	}
	dir, err := legacyWhisperDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, m.Filename), nil
}

// ResolvePath returns where the model actually lives on disk. It prefers the
// canonical path and falls back to the legacy whisper directory so existing
// installs keep working without a re-download.
func ResolvePath(m Model) (string, error) {
	p, err := Path(m)
	if err != nil {
		return "", err
	}
	if existsComplete(m, p) {
		return p, nil
	}
	lp, err := legacyPath(m)
	if err != nil {
		return "", err
	}
	if lp != "" && existsComplete(m, lp) {
		return lp, nil
	}
	return p, nil
}

// existsComplete reports whether a fully usable model exists at root.
func existsComplete(m Model, root string) bool {
	if m.IsArchive() {
		if len(m.Files) == 0 {
			return false // archive with no listed files is never complete
		}
		for _, f := range m.Files {
			st, err := os.Stat(filepath.Join(root, f))
			if err != nil || st.Size() == 0 {
				return false
			}
		}
		return true
	}
	st, err := os.Stat(root)
	return err == nil && st.Size() > 0
}

// IsInstalled reports whether the model is usable from either location.
func IsInstalled(m Model) bool {
	p, err := ResolvePath(m)
	if err != nil {
		return false
	}
	return existsComplete(m, p)
}

// InstalledCount returns how many catalog models are present on disk.
func InstalledCount() int {
	n := 0
	for _, m := range catalog {
		if IsInstalled(m) {
			n++
		}
	}
	return n
}

// InstalledCountForEngine returns how many models of one engine are on disk.
func InstalledCountForEngine(e Engine) int {
	n := 0
	for _, m := range ByEngine(e) {
		if IsInstalled(m) {
			n++
		}
	}
	return n
}

func validateChecksum(gotHex, expected string) error {
	if expected == "" {
		return fmt.Errorf("missing checksum")
	}
	got := strings.ToLower(strings.TrimSpace(gotHex))
	want := strings.ToLower(strings.TrimSpace(expected))
	if got != want {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, want)
	}
	return nil
}

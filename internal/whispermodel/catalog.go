package whispermodel

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// DefaultID is the model Vox uses when no override exists in env or prefs.
const DefaultID = "base.en"

// Model describes one downloadable whisper.cpp GGML model.
type Model struct {
	ID       string
	Label    string
	Filename string
	URL      string
	Checksum string
	SizeMB   int
}

// All checksums are SHA-256, sourced from HuggingFace Git LFS pointers:
// https://huggingface.co/ggerganov/whisper.cpp/tree/main
var catalog = []Model{
	{
		ID:       "tiny.en",
		Label:    "Tiny (English, ~75 MiB)",
		Filename: "ggml-tiny.en.bin",
		URL:      "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.en.bin",
		Checksum: "921e4cf8686fdd993dcd081a5da5b6c365bfde1162e72b08d75ac75289920b1f",
		SizeMB:   75,
	},
	{
		ID:       "base.en",
		Label:    "Base (English, ~142 MiB, default)",
		Filename: "ggml-base.en.bin",
		URL:      "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin",
		Checksum: "a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002",
		SizeMB:   142,
	},
	{
		ID:       "small.en",
		Label:    "Small (English, ~466 MiB)",
		Filename: "ggml-small.en.bin",
		URL:      "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.en.bin",
		Checksum: "c6138d6d58ecc8322097e0f987c32f1be8bb0a18532a3f88f734d1bbf9c41e5d",
		SizeMB:   466,
	},
	{
		ID:       "medium.en",
		Label:    "Medium (English, ~1.5 GiB)",
		Filename: "ggml-medium.en.bin",
		URL:      "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-medium.en.bin",
		Checksum: "cc37e93478338ec7700281a7ac30a10128929eb8f427dda2e865faa8f6da4356",
		SizeMB:   1536,
	},
	{
		ID:       "large-v3-turbo",
		Label:    "Large v3 Turbo (Multilingual, ~1.5 GiB)",
		Filename: "ggml-large-v3-turbo.bin",
		URL:      "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3-turbo.bin",
		Checksum: "1fc70f774d38eb169993ac391eea357ef47c88757ef72ee5943879b7e8e2bc69",
		SizeMB:   1536,
	},
}

// All returns a copy of the supported model catalog.
func All() []Model {
	return slices.Clone(catalog)
}

// ByID resolves a model by stable ID (for prefs/env values).
func ByID(id string) (Model, bool) {
	for _, m := range catalog {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// ModelDir is the download/cache directory for GGML model files.
// Honors WHISPER_MODEL_DIR for parity with Makefile behavior.
func ModelDir() (string, error) {
	if p := os.Getenv("WHISPER_MODEL_DIR"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "whisper-cpp"), nil
}

// Path returns the target path for the model file under ModelDir.
func Path(m Model) (string, error) {
	dir, err := ModelDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, m.Filename), nil
}

// IsInstalled returns true if the model file exists and is non-empty.
func IsInstalled(m Model) bool {
	path, err := Path(m)
	if err != nil {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
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

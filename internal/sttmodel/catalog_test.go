package sttmodel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestByIDKnownModels(t *testing.T) {
	for _, id := range []string{"base.en", "tiny.en", "parakeet-v2", "parakeet-v3"} {
		if _, ok := ByID(id); !ok {
			t.Errorf("ByID(%q) not found", id)
		}
	}
	if _, ok := ByID("nope"); ok {
		t.Error("ByID(\"nope\") should not be found")
	}
}

func TestDefaultIDIsWhisperBase(t *testing.T) {
	if DefaultID != "base.en" {
		t.Errorf("DefaultID = %q, want base.en", DefaultID)
	}
	m, ok := ByID(DefaultID)
	if !ok {
		t.Fatal("default model not in catalog")
	}
	if m.Engine != EngineWhisper {
		t.Errorf("default engine = %q, want %q", m.Engine, EngineWhisper)
	}
}

func TestParakeetModelsAreArchives(t *testing.T) {
	m, _ := ByID("parakeet-v2")
	if m.Engine != EngineParakeet {
		t.Errorf("engine = %q, want %q", m.Engine, EngineParakeet)
	}
	if !m.IsArchive() {
		t.Error("parakeet-v2 should be an archive model")
	}
	want := []string{"encoder.int8.onnx", "decoder.int8.onnx", "joiner.int8.onnx", "tokens.txt"}
	if len(m.Files) != len(want) {
		t.Fatalf("Files = %v, want %v", m.Files, want)
	}
	if m.Checksum != "157c157bc51155e03e37d2466522a3a737dd9c72bb25f36eb18912964161e1ad" {
		t.Errorf("unexpected v2 checksum %q", m.Checksum)
	}
}

func TestWhisperModelsAreSingleFile(t *testing.T) {
	m, _ := ByID("base.en")
	if m.IsArchive() {
		t.Error("base.en should not be an archive model")
	}
	if m.Filename != "ggml-base.en.bin" {
		t.Errorf("Filename = %q", m.Filename)
	}
}

func TestPathAndEngineDirs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)

	w, _ := ByID("base.en")
	gotW, err := Path(w)
	if err != nil {
		t.Fatal(err)
	}
	wantW := filepath.Join(dir, "whisper", "ggml-base.en.bin")
	if gotW != wantW {
		t.Errorf("whisper path = %q, want %q", gotW, wantW)
	}

	p, _ := ByID("parakeet-v2")
	gotP, err := Path(p)
	if err != nil {
		t.Fatal(err)
	}
	wantP := filepath.Join(dir, "parakeet", "sherpa-onnx-nemo-parakeet-tdt-0.6b-v2-int8")
	if gotP != wantP {
		t.Errorf("parakeet path = %q, want %q", gotP, wantP)
	}
}

func TestIsInstalledSingleFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)
	// Isolate the legacy dir too: without this the whisper fallback finds a
	// real ~/.local/share/whisper-cpp install on a developer machine and the
	// "empty dir" assertion below fails for reasons unrelated to the code.
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
	m, _ := ByID("base.en")

	if IsInstalled(m) {
		t.Error("should not be installed in empty dir")
	}
	p, _ := Path(m)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsInstalled(m) {
		t.Error("should be installed after writing file")
	}
}

func TestIsInstalledArchiveRequiresAllFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)
	m, _ := ByID("parakeet-v2")
	root, _ := Path(m)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// Partial extraction: only some required files present.
	for _, f := range m.Files[:2] {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if IsInstalled(m) {
		t.Error("partial extraction should not count as installed")
	}

	for _, f := range m.Files[2:] {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !IsInstalled(m) {
		t.Error("complete extraction should count as installed")
	}
}

func TestLegacyWhisperDirFallback(t *testing.T) {
	newDir := t.TempDir()
	legacy := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", newDir)
	t.Setenv("WHISPER_MODEL_DIR", legacy)

	m, _ := ByID("base.en")
	legacyPath := filepath.Join(legacy, "ggml-base.en.bin")
	if err := os.WriteFile(legacyPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !IsInstalled(m) {
		t.Error("legacy-installed model should be detected")
	}
	got, err := ResolvePath(m)
	if err != nil {
		t.Fatal(err)
	}
	if got != legacyPath {
		t.Errorf("ResolvePath = %q, want legacy %q", got, legacyPath)
	}
}

func TestRemoveAtLegacyPath(t *testing.T) {
	newDir := t.TempDir()
	legacy := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", newDir)
	t.Setenv("WHISPER_MODEL_DIR", legacy)

	m, _ := ByID("base.en")
	legacyFile := filepath.Join(legacy, "ggml-base.en.bin")
	if err := os.WriteFile(legacyFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsInstalled(m) {
		t.Fatal("model should be installed at legacy path")
	}
	if err := Remove(m); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if IsInstalled(m) {
		t.Error("model should no longer be installed after Remove")
	}
}

func TestInstalledCountRespectsEngine(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())

	if got := InstalledCount(); got != 0 {
		t.Errorf("InstalledCount = %d, want 0 in an empty dir", got)
	}

	// Install one whisper model.
	w, _ := ByID("base.en")
	wp, _ := Path(w)
	if err := os.MkdirAll(filepath.Dir(wp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := InstalledCount(); got != 1 {
		t.Errorf("InstalledCount = %d, want 1", got)
	}
	if got := InstalledCountForEngine(EngineWhisper); got != 1 {
		t.Errorf("whisper installed = %d, want 1", got)
	}
	if got := InstalledCountForEngine(EngineParakeet); got != 0 {
		t.Errorf("parakeet installed = %d, want 0", got)
	}
}

func TestByEngine(t *testing.T) {
	if got := len(ByEngine(EngineParakeet)); got != 2 {
		t.Errorf("parakeet models = %d, want 2", got)
	}
	if got := len(ByEngine(EngineWhisper)); got != 5 {
		t.Errorf("whisper models = %d, want 5", got)
	}
}

package main

import (
	"os"
	"path/filepath"
	"testing"

	"vox/internal/sttmodel"
)

// isolateModelDirs points both the canonical and the legacy model directories
// at fresh temp dirs. Both are required: this machine may have a real whisper
// install under ~/.local/share/whisper-cpp and a real parakeet install under
// ~/.local/share/vox/models, either of which would leak into these tests.
func isolateModelDirs(t *testing.T) {
	t.Helper()
	t.Setenv("VOX_MODEL_DIR", t.TempDir())
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
}

// installModel writes a placeholder file at the model's canonical path.
func installModel(t *testing.T, id string) sttmodel.Model {
	t.Helper()
	m, ok := sttmodel.ByID(id)
	if !ok {
		t.Fatalf("ByID %s", id)
	}
	p, err := sttmodel.Path(m)
	if err != nil {
		t.Fatalf("Path %s: %v", id, err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", id, err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", id, err)
	}
	return m
}

// installArchiveModel creates the directory and required files that make an
// archive model (parakeet) count as installed, without downloading 460 MiB.
func installArchiveModel(t *testing.T, id string) sttmodel.Model {
	t.Helper()
	m, ok := sttmodel.ByID(id)
	if !ok {
		t.Fatalf("ByID %s", id)
	}
	if !m.IsArchive() {
		t.Fatalf("%s is not an archive model", id)
	}
	dir, err := sttmodel.Path(m)
	if err != nil {
		t.Fatalf("Path %s: %v", id, err)
	}
	for _, f := range m.Files {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}
	return m
}

func TestPickFallbackModel_MultipleInstalled(t *testing.T) {
	isolateModelDirs(t)

	// Install two models: tiny.en and base.en.
	installModel(t, "tiny.en")
	installModel(t, "base.en")

	// Excluding tiny.en should return base.en.
	got, ok := pickFallbackModel("tiny.en", sttmodel.EngineWhisper)
	if !ok {
		t.Fatal("pickFallbackModel(tiny.en): no fallback found")
	}
	if got.ID != "base.en" {
		t.Errorf("expected base.en, got %s", got.ID)
	}

	// Excluding base.en should return tiny.en.
	got, ok = pickFallbackModel("base.en", sttmodel.EngineWhisper)
	if !ok {
		t.Fatal("pickFallbackModel(base.en): no fallback found")
	}
	if got.ID != "tiny.en" {
		t.Errorf("expected tiny.en, got %s", got.ID)
	}
}

func TestPickFallbackModel_OnlyOneInstalled(t *testing.T) {
	isolateModelDirs(t)

	// Install only base.en.
	installModel(t, "base.en")

	// Excluding base.en should fail — no installed fallback.
	if _, ok := pickFallbackModel("base.en", sttmodel.EngineWhisper); ok {
		t.Fatal("expected no fallback when excluding the only installed model")
	}
}

func TestPickFallbackModel_NoneInstalled(t *testing.T) {
	isolateModelDirs(t)

	if _, ok := pickFallbackModel("base.en", sttmodel.EngineWhisper); ok {
		t.Fatal("expected no fallback when no models are installed")
	}
}

// TestPickFallbackModel_PrefersSameEngine is the discriminating test for the
// preferred-engine parameter: whisper models come first in catalog order, so
// a plain scan would hand back tiny.en here.
func TestPickFallbackModel_PrefersSameEngine(t *testing.T) {
	isolateModelDirs(t)

	installModel(t, "tiny.en")
	installModel(t, "base.en")
	installArchiveModel(t, "parakeet-v2")
	installArchiveModel(t, "parakeet-v3")

	got, ok := pickFallbackModel("parakeet-v2", sttmodel.EngineParakeet)
	if !ok {
		t.Fatal("expected a fallback")
	}
	if got.ID != "parakeet-v3" {
		t.Errorf("got %s, want parakeet-v3 (same engine preferred over whisper)", got.ID)
	}
}

// TestPickFallbackModel_CrossesEngineWhenNoSameEngineOption confirms the
// preference is a preference, not a hard requirement: with no other parakeet
// model installed, a whisper model is better than nothing.
func TestPickFallbackModel_CrossesEngineWhenNoSameEngineOption(t *testing.T) {
	isolateModelDirs(t)

	installModel(t, "base.en")
	installArchiveModel(t, "parakeet-v2")

	got, ok := pickFallbackModel("parakeet-v2", sttmodel.EngineParakeet)
	if !ok {
		t.Fatal("expected a cross-engine fallback")
	}
	if got.ID != "base.en" {
		t.Errorf("got %s, want base.en", got.ID)
	}
}

func TestBuildModelRemovePresets_LastModelProtected(t *testing.T) {
	isolateModelDirs(t)

	// Install one model.
	installModel(t, "base.en")

	presets := buildModelRemovePresets()
	if len(presets) != 1 {
		t.Fatalf("expected 1 preset, got %d", len(presets))
	}
	if presets[0].Removable {
		t.Error("last installed model should not be removable")
	}
	if presets[0].ID != "base.en" {
		t.Errorf("expected base.en, got %s", presets[0].ID)
	}
}

func TestBuildModelRemovePresets_MultipleRemovable(t *testing.T) {
	isolateModelDirs(t)

	// Install two models.
	installModel(t, "tiny.en")
	installModel(t, "base.en")

	presets := buildModelRemovePresets()
	if len(presets) != 2 {
		t.Fatalf("expected 2 presets, got %d", len(presets))
	}
	for _, p := range presets {
		if !p.Removable {
			t.Errorf("model %s should be removable when 2 are installed", p.ID)
		}
	}
}

// TestBuildModelPresetsGroupsByEngine locks in the ordering the menubar
// depends on: the submenu renders one contiguous section per engine, which
// only works if the catalog never interleaves them.
func TestBuildModelPresetsGroupsByEngine(t *testing.T) {
	isolateModelDirs(t)

	got := buildModelPresets()
	if len(got) != len(sttmodel.All()) {
		t.Fatalf("presets = %d, want %d", len(got), len(sttmodel.All()))
	}

	// Whisper models must all precede parakeet models so the submenu renders
	// as two contiguous sections.
	sawWhisper, sawParakeet := false, false
	for _, p := range got {
		switch p.Engine {
		case string(sttmodel.EngineParakeet):
			sawParakeet = true
		case string(sttmodel.EngineWhisper):
			sawWhisper = true
			if sawParakeet {
				t.Errorf("whisper model %q appears after a parakeet model", p.ID)
			}
		default:
			t.Errorf("model %q has unknown engine %q", p.ID, p.Engine)
		}
	}
	if !sawWhisper {
		t.Error("no whisper models in presets")
	}
	if !sawParakeet {
		t.Error("no parakeet models in presets")
	}
}

func TestBuildModelPresetsMarksInstalled(t *testing.T) {
	isolateModelDirs(t)

	for _, p := range buildModelPresets() {
		if p.Installed {
			t.Errorf("model %q reported installed in empty dir", p.ID)
		}
	}

	installModel(t, "base.en")

	var found bool
	for _, p := range buildModelPresets() {
		if p.ID == "base.en" {
			found = true
			if !p.Installed {
				t.Error("base.en should be marked installed")
			}
		}
	}
	if !found {
		t.Error("base.en missing from presets")
	}
}

func TestBuildModelRemovePresetsOnlyInstalled(t *testing.T) {
	isolateModelDirs(t)

	if got := buildModelRemovePresets(); len(got) != 0 {
		t.Errorf("remove presets = %d, want 0 when nothing installed", len(got))
	}

	installArchiveModel(t, "parakeet-v2")

	got := buildModelRemovePresets()
	if len(got) != 1 {
		t.Fatalf("remove presets = %d, want 1", len(got))
	}
	if got[0].ID != "parakeet-v2" {
		t.Errorf("ID = %q, want parakeet-v2", got[0].ID)
	}
	// Sole installed model must not be removable.
	if got[0].Removable {
		t.Error("last installed model should not be removable")
	}
}

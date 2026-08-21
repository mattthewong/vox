package main

import (
	"os"
	"testing"

	"path/filepath"

	"vox/internal/sttmodel"
)

func TestPickFallbackModel_MultipleInstalled(t *testing.T) {
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
	t.Setenv("VOX_MODEL_DIR", t.TempDir())

	// Install two models: tiny.en and base.en.
	tiny, ok := sttmodel.ByID("tiny.en")
	if !ok {
		t.Fatal("ByID tiny.en")
	}
	base, ok := sttmodel.ByID("base.en")
	if !ok {
		t.Fatal("ByID base.en")
	}
	for _, m := range []sttmodel.Model{tiny, base} {
		p, err := sttmodel.Path(m)
		if err != nil {
			t.Fatalf("Path %s: %v", m.ID, err)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", m.ID, err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", m.ID, err)
		}
	}

	// Excluding tiny.en should return base.en.
	got, err := pickFallbackModel("tiny.en")
	if err != nil {
		t.Fatalf("pickFallbackModel(tiny.en): %v", err)
	}
	if got.ID != "base.en" {
		t.Errorf("expected base.en, got %s", got.ID)
	}

	// Excluding base.en should return tiny.en.
	got, err = pickFallbackModel("base.en")
	if err != nil {
		t.Fatalf("pickFallbackModel(base.en): %v", err)
	}
	if got.ID != "tiny.en" {
		t.Errorf("expected tiny.en, got %s", got.ID)
	}
}

func TestPickFallbackModel_OnlyOneInstalled(t *testing.T) {
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
	t.Setenv("VOX_MODEL_DIR", t.TempDir())

	// Install only base.en.
	base, ok := sttmodel.ByID("base.en")
	if !ok {
		t.Fatal("ByID base.en")
	}
	p, err := sttmodel.Path(base)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Excluding base.en should fail — no installed fallback.
	_, err = pickFallbackModel("base.en")
	if err == nil {
		t.Fatal("expected error when excluding the only installed model")
	}
}

func TestPickFallbackModel_NoneInstalled(t *testing.T) {
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
	t.Setenv("VOX_MODEL_DIR", t.TempDir())

	_, err := pickFallbackModel("base.en")
	if err == nil {
		t.Fatal("expected error when no models are installed")
	}
}

func TestBuildModelRemovePresets_LastModelProtected(t *testing.T) {
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
	t.Setenv("VOX_MODEL_DIR", t.TempDir())

	// Install one model.
	base, ok := sttmodel.ByID("base.en")
	if !ok {
		t.Fatal("ByID base.en")
	}
	p, err := sttmodel.Path(base)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

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
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
	t.Setenv("VOX_MODEL_DIR", t.TempDir())

	// Install two models.
	for _, id := range []string{"tiny.en", "base.en"} {
		m, ok := sttmodel.ByID(id)
		if !ok {
			t.Fatalf("ByID %s", id)
		}
		p, err := sttmodel.Path(m)
		if err != nil {
			t.Fatalf("Path %s: %v", id, err)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", m.ID, err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", id, err)
		}
	}

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

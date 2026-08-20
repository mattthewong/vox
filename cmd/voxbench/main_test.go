package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifestResolvesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	mp := filepath.Join(dir, "manifest.jsonl")
	body := `{"wav":"clips/a.wav","text":"hello world"}
{"wav":"clips/b.wav","text":"second clip"}
`
	if err := os.WriteFile(mp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := loadManifest(mp)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
	want := filepath.Join(dir, "clips", "a.wav")
	if got[0].WAV != want {
		t.Errorf("WAV = %q, want %q", got[0].WAV, want)
	}
	if got[0].Text != "hello world" {
		t.Errorf("Text = %q", got[0].Text)
	}
}

func TestLoadManifestKeepsAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	mp := filepath.Join(dir, "m.jsonl")
	abs := "/tmp/somewhere/x.wav"
	if err := os.WriteFile(mp, []byte(`{"wav":"`+abs+`","text":"t"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadManifest(mp)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].WAV != abs {
		t.Errorf("WAV = %q, want %q unchanged", got[0].WAV, abs)
	}
}

func TestLoadManifestSkipsBlanksAndComments(t *testing.T) {
	dir := t.TempDir()
	mp := filepath.Join(dir, "m.jsonl")
	body := `# a comment

{"wav":"a.wav","text":"one"}

# another
{"wav":"b.wav","text":"two"}
`
	if err := os.WriteFile(mp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadManifest(mp)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("entries = %d, want 2", len(got))
	}
}

func TestLoadManifestReportsLineNumberOnBadJSON(t *testing.T) {
	dir := t.TempDir()
	mp := filepath.Join(dir, "m.jsonl")
	body := `{"wav":"a.wav","text":"ok"}
{not json}
`
	if err := os.WriteFile(mp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadManifest(mp)
	if err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should name the offending line, got: %v", err)
	}
}

func TestLoadManifestMissingFile(t *testing.T) {
	if _, err := loadManifest(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Error("expected error for missing manifest")
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
		{1536 * 1024 * 1024, "1.5 GiB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.in); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

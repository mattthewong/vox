package sttmodel

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// buildTestArchive builds an in-memory .tar.bz2-shaped payload for tests.
// bzip2 compression is not available in the stdlib for writing, so tests use
// an uncompressed tar; extractArchive sniffs the format.
func buildTestArchive(t *testing.T, root string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     root + "/",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	}); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: root + "/" + name,
			Mode: 0o644,
			Size: int64(len(body)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDownloadSingleFileVerifiesChecksum(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)
	t.Setenv("WHISPER_MODEL_DIR", filepath.Join(dir, "legacy-empty"))

	payload := []byte("fake ggml model bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	m := Model{
		ID:       "test-single",
		Engine:   EngineWhisper,
		Filename: "test.bin",
		URL:      srv.URL,
		Checksum: sha256Hex(payload),
	}

	var lastPct int64
	if err := Download(context.Background(), m, func(done, total int64) {
		if total > 0 {
			lastPct = done * 100 / total
		}
	}); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !IsInstalled(m) {
		t.Error("model should be installed after download")
	}
	if lastPct != 100 {
		t.Errorf("final progress = %d%%, want 100%%", lastPct)
	}

	p, _ := Path(m)
	got, _ := os.ReadFile(p)
	if string(got) != string(payload) {
		t.Errorf("content mismatch")
	}
}

func TestDownloadRejectsBadChecksum(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)
	t.Setenv("WHISPER_MODEL_DIR", filepath.Join(dir, "legacy-empty"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("actual bytes"))
	}))
	defer srv.Close()

	m := Model{
		ID:       "test-bad",
		Engine:   EngineWhisper,
		Filename: "bad.bin",
		URL:      srv.URL,
		Checksum: sha256Hex([]byte("different bytes")),
	}

	if err := Download(context.Background(), m, nil); err == nil {
		t.Fatal("expected checksum error, got nil")
	}
	if IsInstalled(m) {
		t.Error("failed download must not leave an installed model")
	}
	p, _ := Path(m)
	if _, err := os.Stat(p + ".part"); !os.IsNotExist(err) {
		t.Error("temp .part file should be cleaned up")
	}
}

func TestDownloadArchiveExtractsAndVerifies(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)
	t.Setenv("WHISPER_MODEL_DIR", filepath.Join(dir, "legacy-empty"))

	archive := buildTestArchive(t, "test-model", map[string]string{
		"encoder.int8.onnx": "enc",
		"decoder.int8.onnx": "dec",
		"joiner.int8.onnx":  "join",
		"tokens.txt":        "tok",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	m := Model{
		ID:       "test-archive",
		Engine:   EngineParakeet,
		Dirname:  "test-model",
		URL:      srv.URL,
		Checksum: sha256Hex(archive),
		Files:    []string{"encoder.int8.onnx", "decoder.int8.onnx", "joiner.int8.onnx", "tokens.txt"},
	}

	if err := Download(context.Background(), m, nil); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !IsInstalled(m) {
		t.Fatal("archive model should be installed")
	}
	root, _ := Path(m)
	got, err := os.ReadFile(filepath.Join(root, "tokens.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "tok" {
		t.Errorf("tokens.txt = %q, want %q", got, "tok")
	}
}

func TestDownloadArchiveRejectsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)
	t.Setenv("WHISPER_MODEL_DIR", filepath.Join(dir, "legacy-empty"))

	// Archive is missing joiner.int8.onnx.
	archive := buildTestArchive(t, "incomplete", map[string]string{
		"encoder.int8.onnx": "enc",
		"decoder.int8.onnx": "dec",
		"tokens.txt":        "tok",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	m := Model{
		ID:       "test-incomplete",
		Engine:   EngineParakeet,
		Dirname:  "incomplete",
		URL:      srv.URL,
		Checksum: sha256Hex(archive),
		Files:    []string{"encoder.int8.onnx", "decoder.int8.onnx", "joiner.int8.onnx", "tokens.txt"},
	}

	if err := Download(context.Background(), m, nil); err == nil {
		t.Fatal("expected missing-file error, got nil")
	}
	if IsInstalled(m) {
		t.Error("incomplete extraction must not count as installed")
	}
	root, _ := Path(m)
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Error("incomplete extraction directory should be removed")
	}
}

func TestConcurrentDownloadsRunOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)
	t.Setenv("WHISPER_MODEL_DIR", filepath.Join(dir, "legacy-empty"))

	payload := []byte("model bytes")
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		time.Sleep(50 * time.Millisecond) // widen the race window
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	m := Model{
		ID:       "test-concurrent",
		Engine:   EngineWhisper,
		Filename: "concurrent.bin",
		URL:      srv.URL,
		Checksum: sha256Hex(payload),
	}

	const callers = 5
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Download(context.Background(), m, nil)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("Download: %v", err)
		}
	}
	// The per-destination lock plus the re-check inside it must collapse
	// five callers into one fetch. More than one means the guard is broken
	// and users on a slow link would pull 460 MiB twice.
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server hits = %d, want 1", got)
	}
	if !IsInstalled(m) {
		t.Error("model should be installed")
	}
}

func TestDownloadCleansUpOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)
	t.Setenv("WHISPER_MODEL_DIR", filepath.Join(dir, "legacy-empty"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 100; i++ {
			_, _ = w.Write(make([]byte, 10000))
			w.(http.Flusher).Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer srv.Close()

	m := Model{
		ID:       "test-cancel",
		Engine:   EngineWhisper,
		Filename: "cancel.bin",
		URL:      srv.URL,
		Checksum: sha256Hex([]byte("irrelevant")),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	if err := Download(ctx, m, nil); err == nil {
		t.Fatal("expected an error from the cancelled download")
	}
	if IsInstalled(m) {
		t.Error("cancelled download must not leave an installed model")
	}
	p, _ := Path(m)
	if _, err := os.Stat(p + ".part"); !os.IsNotExist(err) {
		t.Error("temp .part file should be cleaned up after cancellation")
	}
}

func TestRemoveArchiveModel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)
	t.Setenv("WHISPER_MODEL_DIR", filepath.Join(dir, "legacy-empty"))

	m, _ := ByID("parakeet-v2")
	root, _ := Path(m)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range m.Files {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !IsInstalled(m) {
		t.Fatal("setup failed")
	}
	if err := Remove(m); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if IsInstalled(m) {
		t.Error("model should be gone after Remove")
	}
}

// TestStripTopLevelAbsorbsOneLevel documents that stripTopLevel's
// Clean-then-drop-first-component already neutralises a single "..", so
// "root/../../escape.txt" lands harmlessly inside destDir rather than beside
// it. safeJoin is the backstop for anything deeper.
func TestStripTopLevelAbsorbsOneLevel(t *testing.T) {
	if got := stripTopLevel("root/../../escape.txt"); got != "escape.txt" {
		t.Errorf("stripTopLevel = %q, want %q", got, "escape.txt")
	}
	if got := stripTopLevel("model/encoder.int8.onnx"); got != "encoder.int8.onnx" {
		t.Errorf("stripTopLevel = %q, want %q", got, "encoder.int8.onnx")
	}
	if got := stripTopLevel("model/"); got != "" {
		t.Errorf("stripTopLevel of root entry = %q, want empty", got)
	}
}

// TestExtractArchiveRejectsPathTraversal covers the safeJoin guard directly:
// a malicious archive entry must not be able to write outside destDir.
func TestExtractArchiveRejectsPathTraversal(t *testing.T) {
	tmp := t.TempDir()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := "pwned"
	// Three "..' segments: one is consumed by the top-level strip, the rest
	// must be caught by safeJoin.
	if err := tw.WriteHeader(&tar.Header{
		Name: "root/../../../escape.txt",
		Mode: 0o644,
		Size: int64(len(body)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(tmp, "evil.tar")
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(tmp, "nested", "dest")
	if err := extractArchive(archivePath, dest); err == nil {
		t.Fatal("expected extractArchive to reject a traversal entry")
	}
	if _, err := os.Stat(filepath.Join(tmp, "escape.txt")); !os.IsNotExist(err) {
		t.Error("traversal entry escaped the destination directory")
	}
}

func TestRemoveIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())

	// Removing a model that was never installed must not error, for either
	// shape. Callers should not have to guard with IsInstalled.
	single, _ := ByID("base.en")
	if err := Remove(single); err != nil {
		t.Errorf("Remove(uninstalled single-file) = %v, want nil", err)
	}
	archive, _ := ByID("parakeet-v2")
	if err := Remove(archive); err != nil {
		t.Errorf("Remove(uninstalled archive) = %v, want nil", err)
	}

	// And removing twice after a real install is also fine.
	p, _ := Path(single)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(single); err != nil {
		t.Fatalf("first Remove: %v", err)
	}
	if err := Remove(single); err != nil {
		t.Errorf("second Remove = %v, want nil", err)
	}
}

package whispermodel

import (
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

func TestByID(t *testing.T) {
	if _, ok := ByID(DefaultID); !ok {
		t.Fatalf("ByID(%q) not found", DefaultID)
	}
	if _, ok := ByID("nope"); ok {
		t.Fatal("ByID(nope) unexpectedly found model")
	}
}

func TestInstalledCount(t *testing.T) {
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
	if got := InstalledCount(); got != 0 {
		t.Fatalf("InstalledCount with empty dir = %d, want 0", got)
	}
	tiny, ok := ByID("tiny.en")
	if !ok {
		t.Fatal("ByID tiny.en")
	}
	pathTiny, err := Path(tiny)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.WriteFile(pathTiny, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := InstalledCount(); got != 1 {
		t.Fatalf("InstalledCount one catalog model = %d, want 1", got)
	}
	base, ok := ByID("base.en")
	if !ok {
		t.Fatal("ByID base.en")
	}
	pathBase, err := Path(base)
	if err != nil {
		t.Fatalf("Path base: %v", err)
	}
	if err := os.WriteFile(pathBase, []byte("y"), 0o600); err != nil {
		t.Fatalf("WriteFile base: %v", err)
	}
	if got := InstalledCount(); got != 2 {
		t.Fatalf("InstalledCount two catalog models = %d, want 2", got)
	}
}

func TestPathAndIsInstalled(t *testing.T) {
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
	m := Model{Filename: "ggml-foo.bin"}
	path, err := Path(m)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if IsInstalled(m) {
		t.Fatal("IsInstalled true before file exists")
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !IsInstalled(m) {
		t.Fatal("IsInstalled false after file write")
	}
}

func TestRemove(t *testing.T) {
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
	m := Model{Filename: "ggml-foo.bin"}
	path, err := Path(m)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	part := path + ".part"
	if err := os.WriteFile(path, []byte("installed"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(part, []byte("partial"), 0o600); err != nil {
		t.Fatalf("WriteFile partial: %v", err)
	}
	if err := Remove(m); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected model file gone after Remove")
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal("expected .part file gone after Remove")
	}
	if err := Remove(m); err != nil {
		t.Fatalf("Remove idempotent: %v", err)
	}
}

func TestDownloadSuccess(t *testing.T) {
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
	payload := []byte("vox-test-model")
	sum := sha256.Sum256(payload)
	checksum := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	m := Model{
		ID:       "test",
		Filename: "ggml-test.bin",
		URL:      srv.URL,
		Checksum: checksum,
	}
	var updates int
	if err := Download(context.Background(), m, func(downloaded, total int64) {
		updates++
		if downloaded < 0 {
			t.Errorf("downloaded < 0: %d", downloaded)
		}
		_ = total
	}); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if updates == 0 {
		t.Fatal("expected at least one progress update")
	}
	path, _ := Path(m)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("file contents mismatch: got %q want %q", string(got), string(payload))
	}
}

func TestDownloadChecksumMismatch(t *testing.T) {
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("bad"))
	}))
	defer srv.Close()

	m := Model{
		ID:       "test",
		Filename: "ggml-test.bin",
		URL:      srv.URL,
		Checksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	err := Download(context.Background(), m, nil)
	if err == nil {
		t.Fatal("expected checksum mismatch")
	}
	path, _ := Path(m)
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected no final file after mismatch, stat err = %v", statErr)
	}
	part := path + ".part"
	if _, statErr := os.Stat(part); !os.IsNotExist(statErr) {
		t.Fatalf("expected no .part file after mismatch, stat err = %v", statErr)
	}
}

func TestModelDirDefault(t *testing.T) {
	t.Setenv("WHISPER_MODEL_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir, err := ModelDir()
	if err != nil {
		t.Fatalf("ModelDir: %v", err)
	}
	want := filepath.Join(home, ".local", "share", "whisper-cpp")
	if dir != want {
		t.Fatalf("ModelDir = %q, want %q", dir, want)
	}
}

func TestDownloadConcurrentSameModelIsSerialized(t *testing.T) {
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())
	payload := []byte("vox-concurrent-model")
	sum := sha256.Sum256(payload)
	checksum := hex.EncodeToString(sum[:])

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		time.Sleep(50 * time.Millisecond) // increase overlap window
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	m := Model{
		ID:       "test-concurrent",
		Filename: "ggml-test-concurrent.bin",
		URL:      srv.URL,
		Checksum: checksum,
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			errs <- Download(context.Background(), m, nil)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent download returned error: %v", err)
		}
	}

	if got := requests.Load(); got != 1 {
		t.Fatalf("download requests = %d, want 1", got)
	}
	if !IsInstalled(m) {
		t.Fatal("model should be installed after concurrent download")
	}
}

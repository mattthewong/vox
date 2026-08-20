//go:build smoke_manual

package sttmodel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestRealBzip2ArchiveEndToEnd closes the coverage gap called out in Task 5:
// unit tests build uncompressed tars (Go cannot write bzip2), so the
// bzip2.NewReader branch of extractArchive is exercised by nothing. This
// serves the genuine sherpa-onnx release archive over HTTP and drives the
// full path: download -> checksum -> bzip2 detect -> extract -> flatten one
// level -> verify required files -> atomic install.
//
// Requires VOX_TEST_BZ2 to point at a real .tar.bz2 release archive:
//
//	curl -L -o /tmp/v3.tar.bz2 \
//	  https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8.tar.bz2
//	VOX_TEST_BZ2=/tmp/v3.tar.bz2 go test -tags smoke_manual ./internal/sttmodel/ -run RealBzip2 -v
//
// Tagged out of the default suite because it needs a 465 MiB archive and
// takes ~30s.
func TestRealBzip2ArchiveEndToEnd(t *testing.T) {
	src := os.Getenv("VOX_TEST_BZ2")
	if src == "" {
		t.Skip("VOX_TEST_BZ2 unset")
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("cannot read %s: %v", src, err)
	}
	if len(data) < 3 || data[0] != 'B' || data[1] != 'Z' || data[2] != 'h' {
		t.Fatalf("%s is not bzip2 (magic %q)", src, data[:3])
	}

	dir := t.TempDir()
	t.Setenv("VOX_MODEL_DIR", dir)
	t.Setenv("WHISPER_MODEL_DIR", t.TempDir())

	// Set Content-Length explicitly. Without it, net/http chunk-encodes a body
	// this large and ContentLength is -1, which is a property of httptest, not
	// of the real GitHub release (verified: it sends Content-Length).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	m, ok := ByID("parakeet-v3")
	if !ok {
		t.Fatal("parakeet-v3 not in catalog")
	}
	m.URL = srv.URL // real bytes, real checksum from the catalog

	var lastPct int64
	var lastDone, sawTotal int64
	if err := Download(context.Background(), m, func(done, total int64) {
		lastDone, sawTotal = done, total
		if total > 0 {
			lastPct = done * 100 / total
		}
	}); err != nil {
		t.Fatalf("Download: %v", err)
	}

	// Byte count is the assertion that always holds; the percentage only
	// exists when the server advertises a length.
	if lastDone != int64(len(data)) {
		t.Errorf("final progress bytes = %d, want %d", lastDone, len(data))
	}
	if sawTotal > 0 && lastPct != 100 {
		t.Errorf("final progress = %d%%, want 100%%", lastPct)
	}
	if !IsInstalled(m) {
		t.Fatal("model should be installed after bzip2 extraction")
	}

	root, _ := Path(m)
	for _, f := range m.Files {
		st, err := os.Stat(filepath.Join(root, f))
		if err != nil {
			t.Errorf("missing %s: %v", f, err)
			continue
		}
		if st.Size() == 0 {
			t.Errorf("%s is empty", f)
		}
		t.Logf("extracted %-22s %10d bytes", f, st.Size())
	}

	// The archive nests everything under a top-level dir; stripTopLevel must
	// have flattened exactly one level, not zero and not two.
	if _, err := os.Stat(filepath.Join(root, m.Dirname)); err == nil {
		t.Error("top-level directory was not flattened (double nesting)")
	}
	// Staging must not survive a successful install.
	if _, err := os.Stat(root + ".staging"); !os.IsNotExist(err) {
		t.Error("staging directory left behind")
	}
	if _, err := os.Stat(root + ".part"); !os.IsNotExist(err) {
		t.Error(".part file left behind")
	}
}

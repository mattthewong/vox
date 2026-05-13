package whispermodel

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var downloadLocks sync.Map // map[string]*sync.Mutex

// maxDownloadBytes caps the response body to prevent a compromised CDN from
// filling disk. Set to 2x the largest catalog model (~1.5 GiB) plus margin.
const maxDownloadBytes = 4 * 1024 * 1024 * 1024 // 4 GiB

// downloadHTTPClient is a dedicated client with a generous but finite timeout
// for model downloads. Using http.DefaultClient risks no timeout and inherits
// application-global transport changes.
var downloadHTTPClient = &http.Client{
	Timeout: 30 * time.Minute, // large models on slow connections
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing non-HTTPS redirect to %s", req.URL)
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

// Download fetches a model file to disk, verifies checksum, and atomically
// installs it at the final destination path. Existing valid files are kept.
func Download(ctx context.Context, m Model, onProgress func(downloaded, total int64)) error {
	dest, err := Path(m)
	if err != nil {
		return err
	}
	lock := downloadLock(dest)
	lock.Lock()
	defer lock.Unlock()

	if IsInstalled(m) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := downloadHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("download model: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download model: unexpected status %d", resp.StatusCode)
	}

	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
	}()
	defer func() {
		_ = os.Remove(tmp)
	}()

	hasher, checksumName, err := checksumHash(m.Checksum)
	if err != nil {
		return err
	}
	pw := &progressWriter{
		fn:       onProgress,
		total:    resp.ContentLength,
		interval: 250 * time.Millisecond,
	}
	// Limit the response body to prevent unbounded disk writes.
	limited := io.LimitReader(resp.Body, maxDownloadBytes)
	w := io.MultiWriter(f, hasher, pw)
	if _, err := io.Copy(w, limited); err != nil {
		return fmt.Errorf("write model file: %w", err)
	}
	pw.flush()

	got := hex.EncodeToString(hasher.Sum(nil))
	if err := validateChecksum(got, m.Checksum); err != nil {
		return fmt.Errorf("verify %s: %w", checksumName, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync model file: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	closed = true
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	return nil
}

func downloadLock(dest string) *sync.Mutex {
	lock, _ := downloadLocks.LoadOrStore(dest, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func checksumHash(sum string) (hash.Hash, string, error) {
	switch len(strings.TrimSpace(sum)) {
	case 40:
		return sha1.New(), "sha1", nil
	case 64:
		return sha256.New(), "sha256", nil
	default:
		return nil, "", fmt.Errorf("unsupported checksum length %d", len(strings.TrimSpace(sum)))
	}
}

type progressWriter struct {
	fn       func(downloaded, total int64)
	total    int64
	written  int64
	lastSent time.Time
	interval time.Duration
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n := len(b)
	p.written += int64(n)
	p.maybeSend()
	return n, nil
}

func (p *progressWriter) maybeSend() {
	if p.fn == nil {
		return
	}
	if time.Since(p.lastSent) < p.interval {
		return
	}
	p.lastSent = time.Now()
	p.fn(p.written, p.total)
}

func (p *progressWriter) flush() {
	if p.fn == nil {
		return
	}
	p.fn(p.written, p.total)
}

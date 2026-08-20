package sttmodel

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// maxDownloadBytes caps any single download.
const maxDownloadBytes = 4 * 1024 * 1024 * 1024 // 4 GiB

var downloadLocks sync.Map // dest path -> *sync.Mutex

var httpClient = &http.Client{
	Timeout: 30 * time.Minute,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing non-HTTPS redirect to %s", req.URL)
		}
		return nil
	},
}

// Download fetches a model and installs it at its canonical path. Single-file
// models are written directly; archive models are extracted and their required
// files verified. onProgress may be nil.
//
// Download is a no-op if the model is already installed.
func Download(ctx context.Context, m Model, onProgress func(downloaded, total int64)) error {
	dest, err := Path(m)
	if err != nil {
		return err
	}

	mu := downloadLock(dest)
	mu.Lock()
	defer mu.Unlock()

	// Re-check under the lock: a concurrent caller may have finished.
	if IsInstalled(m) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create model dir: %w", err)
	}

	tmp := dest + ".part"
	defer os.Remove(tmp)

	sum, err := fetchToFile(ctx, m.URL, tmp, onProgress)
	if err != nil {
		return err
	}
	if err := validateChecksum(sum, m.Checksum); err != nil {
		return err
	}

	if !m.IsArchive() {
		return os.Rename(tmp, dest)
	}

	// Extract into a staging dir, verify, then swap into place atomically.
	staging := dest + ".staging"
	os.RemoveAll(staging)
	defer os.RemoveAll(staging)

	if err := extractArchive(tmp, staging); err != nil {
		return err
	}
	if err := verifyFiles(staging, m.Files); err != nil {
		return fmt.Errorf("archive for %s is incomplete: %w", m.ID, err)
	}
	os.RemoveAll(dest)
	if err := os.Rename(staging, dest); err != nil {
		return fmt.Errorf("install extracted model: %w", err)
	}
	return nil
}

// fetchToFile downloads url into path and returns the hex SHA-256 of the body.
func fetchToFile(ctx context.Context, url, path string, onProgress func(int64, int64)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()

	var hasher hash.Hash = sha256.New()
	pw := &progressWriter{
		fn:       onProgress,
		total:    resp.ContentLength,
		interval: 250 * time.Millisecond,
	}
	if _, err := io.Copy(io.MultiWriter(f, hasher, pw), io.LimitReader(resp.Body, maxDownloadBytes)); err != nil {
		return "", fmt.Errorf("write download: %w", err)
	}
	pw.flush()

	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("sync: %w", err)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

func downloadLock(dest string) *sync.Mutex {
	v, _ := downloadLocks.LoadOrStore(dest, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// progressWriter reports bytes written, throttled to interval.
type progressWriter struct {
	fn       func(downloaded, total int64)
	total    int64
	written  int64
	lastSent time.Time
	interval time.Duration
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.written += int64(len(b))
	if p.fn != nil && time.Since(p.lastSent) >= p.interval {
		p.lastSent = time.Now()
		p.fn(p.written, p.total)
	}
	return len(b), nil
}

// flush emits a final progress callback so consumers always see completion.
func (p *progressWriter) flush() {
	if p.fn != nil {
		p.fn(p.written, p.total)
	}
}

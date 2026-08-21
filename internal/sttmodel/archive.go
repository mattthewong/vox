package sttmodel

import (
	"archive/tar"
	"compress/bzip2"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxArchiveEntryBytes caps a single extracted file to guard against
// decompression bombs. The largest real entry is the ~500 MiB encoder.
const maxArchiveEntryBytes = 2 * 1024 * 1024 * 1024 // 2 GiB

// extractArchive unpacks a tar archive (optionally bzip2-compressed) into
// destDir. Entries are flattened by one level: the archive's single top-level
// directory is stripped, so "model-name/encoder.onnx" lands at
// "<destDir>/encoder.onnx".
func extractArchive(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	var r io.Reader = f
	compressed, err := isBzip2(f)
	if err != nil {
		return err
	}
	if compressed {
		r = bzip2.NewReader(f)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		rel := stripTopLevel(hdr.Name)
		if rel == "" {
			continue
		}
		target, err := safeJoin(destDir, rel)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", rel, err)
			}
		case tar.TypeReg:
			if hdr.Size > maxArchiveEntryBytes {
				return fmt.Errorf("archive entry %s too large (%d bytes)", rel, hdr.Size)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkdir parent of %s: %w", rel, err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("create %s: %w", rel, err)
			}
			if _, err := io.Copy(out, io.LimitReader(tr, maxArchiveEntryBytes)); err != nil {
				out.Close()
				return fmt.Errorf("write %s: %w", rel, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close %s: %w", rel, err)
			}
		default:
			// Skip symlinks, devices, and anything else. Model archives only
			// contain regular files and directories.
		}
	}
}

// isBzip2 sniffs the magic bytes and rewinds the file.
func isBzip2(f *os.File) (bool, error) {
	var magic [3]byte
	n, err := f.Read(magic[:])
	if err != nil && n == 0 {
		return false, fmt.Errorf("read magic: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false, fmt.Errorf("rewind: %w", err)
	}
	return n == 3 && magic[0] == 'B' && magic[1] == 'Z' && magic[2] == 'h', nil
}

// stripTopLevel removes the archive's single root directory component.
func stripTopLevel(name string) string {
	name = strings.TrimPrefix(filepath.Clean(name), "./")
	i := strings.Index(name, "/")
	if i < 0 {
		return "" // the root directory entry itself
	}
	return name[i+1:]
}

// safeJoin joins rel onto base, refusing any path that escapes base.
func safeJoin(base, rel string) (string, error) {
	target := filepath.Join(base, rel)
	cleanBase := filepath.Clean(base) + string(os.PathSeparator)
	if !strings.HasPrefix(target, cleanBase) {
		return "", fmt.Errorf("archive entry escapes destination: %q", rel)
	}
	return target, nil
}

// verifyFiles checks that every required file exists and is non-empty.
func verifyFiles(root string, files []string) error {
	for _, f := range files {
		st, err := os.Stat(filepath.Join(root, f))
		if err != nil {
			return fmt.Errorf("missing required file %s: %w", f, err)
		}
		if st.Size() == 0 {
			return fmt.Errorf("required file %s is empty", f)
		}
	}
	return nil
}

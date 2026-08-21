package sttmodel

import "os"

// Remove deletes an installed model from disk, including any leftover
// temp artifacts. Archive models remove their whole directory.
//
// Remove is idempotent: removing a model that is not installed is not an
// error. os.RemoveAll already behaves this way for the archive branch, and
// the single-file branch matches it so callers do not have to guard with
// IsInstalled to avoid a spurious failure.
func Remove(m Model) error {
	p, err := ResolvePath(m)
	if err != nil {
		return err
	}
	// Clean temp artifacts at the resolved path (where the model lives).
	os.Remove(p + ".part")
	os.RemoveAll(p + ".staging")
	// Also clean temp artifacts at the canonical path if it differs
	// (Download creates them there, but ResolvePath may return the legacy path).
	cp, cpErr := Path(m)
	if cpErr == nil && cp != p {
		os.Remove(cp + ".part")
		os.RemoveAll(cp + ".staging")
	}
	if m.IsArchive() {
		return os.RemoveAll(p)
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

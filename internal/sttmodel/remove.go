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
	os.Remove(p + ".part")
	os.RemoveAll(p + ".staging")
	if m.IsArchive() {
		return os.RemoveAll(p)
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

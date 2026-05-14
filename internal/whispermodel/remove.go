package whispermodel

import "os"

// Remove deletes the on-disk model file and any in-progress partial download
// (.part) for this catalog entry. Missing files are not an error.
func Remove(m Model) error {
	path, err := Path(m)
	if err != nil {
		return err
	}
	_ = os.Remove(path + ".part")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

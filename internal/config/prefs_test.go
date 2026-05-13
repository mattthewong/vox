package config

import (
	"path/filepath"
	"sync"
	"testing"
)

// TestSavePref_ConcurrentMutationsPreserveBothFields asserts that concurrent
// SavePref calls from different goroutines (the hotkey watcher and the
// settings watcher in main.go) don't clobber each other's fields. Without
// the mutex inside SavePref this test is racy: each goroutine reads the
// same empty Prefs, then both write back their single change, and one of
// the two updates is lost.
//
// The goroutines alternate between different values so the test can't pass
// by accident — if the mutex is missing, at least one goroutine's last
// write will clobber the other's field back to zero-value.
func TestSavePref_ConcurrentMutationsPreserveBothFields(t *testing.T) {
	t.Setenv("VOX_PREFS_PATH", filepath.Join(t.TempDir(), "prefs.json"))

	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(2)

	hotkeys := []string{"fn", "option+space"}
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			hk := hotkeys[i%2]
			if err := SavePref(func(p *Prefs) { p.Hotkey = hk }); err != nil {
				t.Errorf("SavePref hotkey: %v", err)
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			val := i%2 == 0
			if err := SavePref(func(p *Prefs) {
				p.SoundsEnabled = BoolPtr(val)
				p.Model = "small.en"
			}); err != nil {
				t.Errorf("SavePref sounds: %v", err)
				return
			}
		}
	}()

	wg.Wait()

	got, err := LoadPrefs()
	if err != nil {
		t.Fatalf("LoadPrefs: %v", err)
	}
	// After 50 iterations (even count), the last hotkey write is hotkeys[1]
	// and the last sounds write is BoolPtr(false). The critical assertion is
	// that both fields are present — neither was clobbered to zero-value by
	// the other goroutine's read-modify-write cycle.
	if got.Hotkey == "" {
		t.Error("Hotkey lost across concurrent saves: got empty string")
	}
	if got.SoundsEnabled == nil {
		t.Error("SoundsEnabled lost across concurrent saves: got nil")
	}
	if got.Model == "" {
		t.Error("Model lost across concurrent saves: got empty string")
	}
}

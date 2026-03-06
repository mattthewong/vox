//go:build darwin

package audio

import (
	_ "embed"
	"os"
	"os/exec"
)

//go:embed sounds/start.wav
var startSound []byte

//go:embed sounds/stop.wav
var stopSound []byte

// PlayStartSound plays the recording-start chime.
func PlayStartSound() {
	playEmbedded(startSound)
}

// PlayStopSound plays the recording-stop chime.
func PlayStopSound() {
	playEmbedded(stopSound)
}

func playEmbedded(data []byte) {
	f, err := os.CreateTemp("", "vox-sound-*.wav")
	if err != nil {
		return
	}
	name := f.Name()
	_, _ = f.Write(data)
	f.Close()

	cmd := exec.Command("afplay", name)
	cmd.Start()
	// Clean up after playback in background.
	go func() {
		cmd.Wait()
		os.Remove(name)
	}()
}

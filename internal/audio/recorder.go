package audio

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// ErrTooShort is returned when the recording is too short to be useful.
var ErrTooShort = errors.New("recording too short")

// ErrNotRecording is returned when Stop is called but no recording is in progress.
var ErrNotRecording = errors.New("not recording")

// ErrAlreadyRecording is returned when Start is called while already recording.
var ErrAlreadyRecording = errors.New("already recording")

// toolKind represents the external CLI tool used for recording.
type toolKind int

const (
	toolSox    toolKind = iota // rec (from SoX)
	toolFFmpeg                 // ffmpeg
)

const (
	defaultSampleRate  = 16000
	minFileSize        = 1024              // 1 KB — anything smaller is too short to contain speech
	DefaultMaxDuration = 5 * time.Minute   // safety limit to prevent unbounded disk growth
	killGracePeriod    = 3 * time.Second   // time to wait for SIGINT before escalating to SIGKILL
	tempFilePattern    = "vox-*.wav"       // pattern used for recording temp files
	soundFilePattern   = "vox-sound-*.wav" // pattern used for chime temp files
)

// Recorder manages microphone recording via an external CLI tool (sox or ffmpeg).
type Recorder struct {
	SampleRate  int
	MaxDuration time.Duration // 0 means no limit

	mu        sync.Mutex
	tool      toolKind
	cmd       *exec.Cmd
	tmpPath   string
	recording bool
	startTime time.Time
	timer     *time.Timer // auto-stop timer
}

// CleanupOrphanedTempFiles removes any leftover vox temp files from prior
// crashes or ungraceful shutdowns. Any vox-*.wav file that exists before the
// process starts is definitionally orphaned — no running instance owns it.
// This should be called once at startup.
func CleanupOrphanedTempFiles() (removed int) {
	for _, pattern := range []string{tempFilePattern, soundFilePattern} {
		matches, _ := filepath.Glob(filepath.Join(os.TempDir(), pattern))
		for _, m := range matches {
			if os.Remove(m) == nil {
				removed++
			}
		}
	}
	return removed
}

// NewRecorder creates a Recorder after auto-detecting an available recording tool.
// It checks for sox (via the "rec" command) first, then ffmpeg. If neither is
// found on the system PATH an error is returned.
func NewRecorder() (*Recorder, error) {
	r := &Recorder{
		SampleRate:  defaultSampleRate,
		MaxDuration: DefaultMaxDuration,
	}

	if _, err := exec.LookPath("rec"); err == nil {
		r.tool = toolSox
		return r, nil
	}

	if _, err := exec.LookPath("ffmpeg"); err == nil {
		r.tool = toolFFmpeg
		return r, nil
	}

	return nil, errors.New("no recording tool found: install sox (rec) or ffmpeg")
}

// Start begins recording audio from the default microphone to a temporary WAV file.
// The recording runs in a background subprocess until Stop is called.
func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.recording {
		return ErrAlreadyRecording
	}

	tmpFile, err := os.CreateTemp("", "vox-*.wav")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	// Close the file handle immediately — the subprocess will write to it by path.
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return fmt.Errorf("close temp file: %w", err)
	}
	r.tmpPath = tmpFile.Name()

	sampleRateStr := fmt.Sprintf("%d", r.SampleRate)

	switch r.tool {
	case toolSox:
		r.cmd = exec.Command("rec",
			"-q",
			"-r", sampleRateStr,
			"-c", "1",
			"-b", "16",
			"-t", "wav",
			r.tmpPath,
		)
	case toolFFmpeg:
		r.cmd = exec.Command("ffmpeg",
			"-f", "avfoundation",
			"-i", ":default",
			"-ar", sampleRateStr,
			"-ac", "1",
			"-y",
			r.tmpPath,
		)
	default:
		_ = os.Remove(r.tmpPath)
		return fmt.Errorf("unknown recording tool: %d", r.tool)
	}

	// Suppress stdout/stderr so they don't leak to the caller's terminal.
	r.cmd.Stdout = nil
	r.cmd.Stderr = nil

	if err := r.cmd.Start(); err != nil {
		_ = os.Remove(r.tmpPath)
		r.cmd = nil
		r.tmpPath = ""
		return fmt.Errorf("start recording process: %w", err)
	}

	r.recording = true
	r.startTime = time.Now()

	// Start a safety timer that auto-stops the recording after MaxDuration.
	// This prevents unbounded disk growth if the user forgets to stop (toggle
	// mode) or if the parent process dies and the subprocess keeps running.
	if r.MaxDuration > 0 {
		r.timer = time.AfterFunc(r.MaxDuration, func() {
			// Best-effort: stop the recording. If it already stopped, this is a no-op.
			_, _ = r.Stop()
		})
	}

	return nil
}

// Stop ends the recording, reads the WAV data, and cleans up the temp file.
// Returns ErrNotRecording if no recording is in progress.
// Returns ErrTooShort if the resulting file is too small to contain useful audio.
func (r *Recorder) Stop() ([]byte, error) {
	r.mu.Lock()

	if !r.recording {
		r.mu.Unlock()
		return nil, ErrNotRecording
	}

	// Cancel the safety timer if it hasn't fired yet.
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}

	// Grab the fields we need, then release the lock BEFORE waiting on the
	// subprocess.  This avoids a deadlock when the timer goroutine calls
	// Stop() while another goroutine polls IsRecording().
	cmd := r.cmd
	tmpPath := r.tmpPath

	// Mark as not-recording immediately so callers see the state change
	// even while we wait for the subprocess to exit.
	r.recording = false
	r.cmd = nil
	r.tmpPath = ""
	r.mu.Unlock()

	// Ensure the temp file is always cleaned up.
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	// Gracefully interrupt the recording process so it flushes and finalises the WAV header.
	if cmd.Process != nil {
		if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
			_ = cmd.Process.Kill()
		}
	}

	// Wait for the process to exit with a grace period. If it doesn't exit
	// after SIGINT, escalate to SIGKILL so we never block indefinitely.
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	select {
	case waitErr := <-waitDone:
		if waitErr != nil {
			// sox and ffmpeg exit with non-zero status on SIGINT — that is expected.
			// We only propagate errors that are NOT an ExitError (e.g. process not started).
			var exitErr *exec.ExitError
			if !errors.As(waitErr, &exitErr) {
				return nil, fmt.Errorf("wait for recording process: %w", waitErr)
			}
		}
	case <-time.After(killGracePeriod):
		// Subprocess didn't exit in time — force-kill it.
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-waitDone // reap the process
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("read recorded audio: %w", err)
	}

	if len(data) < minFileSize {
		return nil, ErrTooShort
	}

	return data, nil
}

// IsRecording reports whether a recording is currently in progress.
func (r *Recorder) IsRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recording
}

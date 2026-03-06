package audio

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	defaultSampleRate = 16000
	minFileSize       = 1024 // 1 KB — anything smaller is too short to contain speech
)

// Recorder manages microphone recording via an external CLI tool (sox or ffmpeg).
type Recorder struct {
	SampleRate int

	mu        sync.Mutex
	tool      toolKind
	cmd       *exec.Cmd
	tmpPath   string
	recording bool
	startTime time.Time
}

// NewRecorder creates a Recorder after auto-detecting an available recording tool.
// It checks for sox (via the "rec" command) first, then ffmpeg. If neither is
// found on the system PATH an error is returned.
func NewRecorder() (*Recorder, error) {
	r := &Recorder{
		SampleRate: defaultSampleRate,
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
	return nil
}

// Stop ends the recording, reads the WAV data, and cleans up the temp file.
// Returns ErrNotRecording if no recording is in progress.
// Returns ErrTooShort if the resulting file is too small to contain useful audio.
func (r *Recorder) Stop() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return nil, ErrNotRecording
	}

	// Always reset state when we leave, regardless of outcome.
	defer func() {
		r.recording = false
		r.cmd = nil
		if r.tmpPath != "" {
			_ = os.Remove(r.tmpPath)
			r.tmpPath = ""
		}
	}()

	// Gracefully interrupt the recording process so it flushes and finalises the WAV header.
	if r.cmd.Process != nil {
		if err := r.cmd.Process.Signal(syscall.SIGINT); err != nil {
			// If signalling fails (e.g. process already exited), try to kill it.
			_ = r.cmd.Process.Kill()
		}
	}

	// Wait for the process to exit. An exit caused by SIGINT is expected and not
	// an error for our purposes, so we only surface truly unexpected failures.
	waitErr := r.cmd.Wait()
	if waitErr != nil {
		// sox and ffmpeg exit with non-zero status on SIGINT — that is expected.
		// We only propagate errors that are NOT an ExitError (e.g. process not started).
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			return nil, fmt.Errorf("wait for recording process: %w", waitErr)
		}
	}

	data, err := os.ReadFile(r.tmpPath)
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

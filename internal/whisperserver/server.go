package whisperserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"vox/internal/transcribe"
)

const (
	startTimeout = 45 * time.Second
	stopTimeout  = 10 * time.Second
)

// Server manages a local whisper-server child process.
type Server struct {
	mu      sync.Mutex
	binPath string
	host    string
	port    int
	logPath string
	model   string
	cmd     *exec.Cmd
	done    chan error
	logFile *os.File
	health  *transcribe.Client
}

// New creates a server manager and resolves whisper-server in PATH.
func New(host string, port int, logPath string) (*Server, error) {
	binPath, err := exec.LookPath("whisper-server")
	if err != nil {
		return nil, fmt.Errorf("whisper-server not found in PATH: %w", err)
	}
	url := fmt.Sprintf("http://%s:%d", host, port)
	return &Server{
		binPath: binPath,
		host:    host,
		port:    port,
		logPath: logPath,
		health:  transcribe.NewClient(url),
	}, nil
}

// URL is the local HTTP endpoint this manager controls.
func (s *Server) URL() string {
	return fmt.Sprintf("http://%s:%d", s.host, s.port)
}

// CurrentModel returns the active model path (if running).
func (s *Server) CurrentModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.model
}

// Start launches whisper-server with the provided model path and waits for readiness.
func (s *Server) Start(ctx context.Context, modelPath string) error {
	s.mu.Lock()
	if s.cmd != nil {
		s.mu.Unlock()
		return fmt.Errorf("whisper-server already running")
	}
	cmd, done, lf, err := s.startLocked(modelPath)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.cmd = cmd
	s.done = done
	s.logFile = lf
	s.model = modelPath
	s.mu.Unlock()

	if err := s.waitReady(ctx); err != nil {
		_ = s.Stop(context.Background())
		return err
	}
	return nil
}

// Switch restarts whisper-server using a new model path. If Start fails
// after Stop, it attempts to roll back to the previous model so the server
// is not left in a permanently stopped state.
func (s *Server) Switch(ctx context.Context, modelPath string) error {
	prevModel := s.CurrentModel()
	if err := s.Stop(ctx); err != nil {
		return err
	}
	if err := s.Start(ctx, modelPath); err != nil {
		// Rollback: try to restart with the previous model so the server
		// isn't left dead. If rollback also fails, return the original error.
		if prevModel != "" {
			if rbErr := s.Start(context.Background(), prevModel); rbErr != nil {
				return fmt.Errorf("switch to %s failed: %w (rollback to %s also failed: %v)", modelPath, err, prevModel, rbErr)
			}
			return fmt.Errorf("switch to %s failed (rolled back to previous model): %w", modelPath, err)
		}
		return err
	}
	return nil
}

// Stop gracefully stops whisper-server if it is running.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	cmd := s.cmd
	done := s.done
	lf := s.logFile
	if cmd == nil {
		s.mu.Unlock()
		return nil
	}
	s.cmd = nil
	s.done = nil
	s.logFile = nil
	s.model = ""
	s.mu.Unlock()

	// Always drain the done channel and close the log file, regardless of
	// which error path we take below.
	defer func() {
		if lf != nil {
			_ = lf.Close()
		}
	}()

	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		// SIGTERM failed unexpectedly — still drain the child.
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-done
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, stopTimeout)
	defer cancel()
	select {
	case <-waitCtx.Done():
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-done
	case <-done:
	}
	return nil
}

// startLocked spawns the whisper-server child process. It does NOT use
// exec.CommandContext so that the parent context cancellation cannot
// race with the explicit Stop() lifecycle. The process is managed
// entirely through Stop() which sends SIGTERM to the process group.
func (s *Server) startLocked(modelPath string) (*exec.Cmd, chan error, *os.File, error) {
	if err := os.MkdirAll(filepath.Dir(s.logPath), 0o755); err != nil {
		return nil, nil, nil, err
	}
	lf, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, nil, err
	}
	args := []string{
		"--host", s.host,
		"--port", strconv.Itoa(s.port),
		"--model", modelPath,
	}
	cmd := exec.Command(s.binPath, args...)
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = lf.Close()
		return nil, nil, nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return cmd, done, lf, nil
}

func (s *Server) waitReady(ctx context.Context) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := s.health.HealthCheck(deadlineCtx); err == nil {
			return nil
		}
		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf("whisper-server did not become ready within %s", startTimeout)
		case <-tick.C:
		}
	}
}

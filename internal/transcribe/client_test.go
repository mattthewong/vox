package transcribe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// minimalWAV is the smallest valid WAV file (44-byte header, zero audio frames).
// It is enough to satisfy the API's file-format check in tests.
var minimalWAV = func() []byte {
	// RIFF header + fmt chunk + data chunk with 0 bytes of audio.
	h := make([]byte, 44)
	copy(h[0:4], "RIFF")
	// File size - 8
	le32(h[4:8], 36)
	copy(h[8:12], "WAVE")
	// fmt sub-chunk
	copy(h[12:16], "fmt ")
	le32(h[16:20], 16) // sub-chunk size
	le16(h[20:22], 1)  // PCM
	le16(h[22:24], 1)  // mono
	le32(h[24:28], 16000)
	le32(h[28:32], 32000) // byte rate
	le16(h[32:34], 2)     // block align
	le16(h[34:36], 16)    // bits per sample
	// data sub-chunk
	copy(h[36:40], "data")
	le32(h[40:44], 0)
	return h
}()

func le16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func le32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func TestTranscribe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := NewClient("")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Skip if the Whisper server is not running.
	if err := client.HealthCheck(ctx); err != nil {
		t.Skipf("whisper server not available: %v", err)
	}

	text, err := client.Transcribe(ctx, minimalWAV)
	if err != nil {
		t.Fatalf("Transcribe() error: %v", err)
	}
	// A zero-length WAV will likely produce an empty or near-empty transcription.
	t.Logf("transcription result: %q", text)
}

func TestTranscribeHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	ctx := context.Background()

	_, err := client.Transcribe(ctx, minimalWAV)
	if err == nil {
		t.Fatal("expected error for HTTP 500 response")
	}
	t.Logf("got expected error: %v", err)
}

func TestTranscribeInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	ctx := context.Background()

	_, err := client.Transcribe(ctx, minimalWAV)
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	t.Logf("got expected error: %v", err)
}

func TestTranscribeEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"text": ""}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	ctx := context.Background()

	text, err := client.Transcribe(ctx, minimalWAV)
	if err != nil {
		t.Fatalf("Transcribe() should not error on empty text, got: %v", err)
	}
	if text != "" {
		t.Errorf("expected empty string, got %q", text)
	}
}

func TestHealthCheckOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	if err := client.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error: %v", err)
	}
}

func TestHealthCheckNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	err := client.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error for non-200 health check")
	}
	t.Logf("got expected error: %v", err)
}

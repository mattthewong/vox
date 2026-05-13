package transcribe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

	text, err := client.Transcribe(ctx, minimalWAV, TranscribeOptions{})
	if err != nil {
		// Some local whisper-server builds reject zero-frame WAV payloads.
		// Treat that as an environment limitation, not a regression.
		if strings.Contains(err.Error(), "Invalid request") {
			t.Skipf("server rejected minimal WAV fixture: %v", err)
		}
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

	_, err := client.Transcribe(ctx, minimalWAV, TranscribeOptions{})
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

	_, err := client.Transcribe(ctx, minimalWAV, TranscribeOptions{})
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

	text, err := client.Transcribe(ctx, minimalWAV, TranscribeOptions{})
	if err != nil {
		t.Fatalf("Transcribe() should not error on empty text, got: %v", err)
	}
	if text != "" {
		t.Errorf("expected empty string, got %q", text)
	}
}

func TestTranscribeWithOptions(t *testing.T) {
	var gotPrompt, gotLang string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotPrompt = r.FormValue("initial_prompt")
		gotLang = r.FormValue("language")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text": "hello"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	opts := TranscribeOptions{
		InitialPrompt: "The following terms may appear: flagbearer, gonfalon.",
		Language:      "en",
	}
	text, err := client.Transcribe(context.Background(), minimalWAV, opts)
	if err != nil {
		t.Fatalf("Transcribe() error: %v", err)
	}
	if text != "hello" {
		t.Errorf("unexpected text: %q", text)
	}
	if gotPrompt != opts.InitialPrompt {
		t.Errorf("initial_prompt = %q, want %q", gotPrompt, opts.InitialPrompt)
	}
	if gotLang != "en" {
		t.Errorf("language = %q, want %q", gotLang, "en")
	}
}

func TestTranscribeWithoutOptions(t *testing.T) {
	var hasPrompt, hasLang bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, hasPrompt = r.MultipartForm.Value["initial_prompt"]
		_, hasLang = r.MultipartForm.Value["language"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text": "hello"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Transcribe(context.Background(), minimalWAV, TranscribeOptions{})
	if err != nil {
		t.Fatalf("Transcribe() error: %v", err)
	}
	if hasPrompt {
		t.Error("initial_prompt should not be sent when empty")
	}
	if hasLang {
		t.Error("language should not be sent when empty")
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

func TestResetEndpointClearsCachedDetection(t *testing.T) {
	var openAICalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			if r.ContentLength == 0 {
				// Probe request from resolveEndpoint.
				openAICalls.Add(1)
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"text":"ok"}`))
		case "/inference":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"text":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	if _, err := client.Transcribe(context.Background(), minimalWAV, TranscribeOptions{}); err != nil {
		t.Fatalf("first Transcribe() error: %v", err)
	}
	if openAICalls.Load() != 1 {
		t.Fatalf("probe calls = %d, want 1", openAICalls.Load())
	}

	if _, err := client.Transcribe(context.Background(), minimalWAV, TranscribeOptions{}); err != nil {
		t.Fatalf("second Transcribe() error: %v", err)
	}
	if openAICalls.Load() != 1 {
		t.Fatalf("probe calls after cache = %d, want still 1", openAICalls.Load())
	}

	client.ResetEndpoint()
	if _, err := client.Transcribe(context.Background(), minimalWAV, TranscribeOptions{}); err != nil {
		t.Fatalf("third Transcribe() error: %v", err)
	}
	if openAICalls.Load() != 2 {
		t.Fatalf("probe calls after reset = %d, want 2", openAICalls.Load())
	}
}

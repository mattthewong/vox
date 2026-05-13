package transcribe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultWhisperURL = "http://127.0.0.1:2022"

// Client is an HTTP client for Whisper speech-to-text APIs.
// Supports both OpenAI-compatible (/v1/audio/transcriptions) and whisper.cpp (/inference).
type Client struct {
	whisperURL string
	httpClient *http.Client
	endpoint   string // cached resolved endpoint
	mu         sync.RWMutex
}

// NewClient creates a new transcription client. If whisperURL is empty, it
// defaults to http://127.0.0.1:2022.
func NewClient(whisperURL string) *Client {
	if whisperURL == "" {
		whisperURL = defaultWhisperURL
	}
	// Strip trailing slash to avoid double-slash in URL construction.
	whisperURL = strings.TrimRight(whisperURL, "/")

	return &Client{
		whisperURL: whisperURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// transcriptionResponse represents the JSON body returned by the Whisper API.
type transcriptionResponse struct {
	Text string `json:"text"`
}

// TranscribeOptions holds optional parameters for transcription.
type TranscribeOptions struct {
	InitialPrompt string // Vocabulary hint for whisper.cpp
	Language      string // BCP-47 language code (e.g., "en")
}

// Transcribe sends WAV audio data to the Whisper API and returns the transcribed text.
func (c *Client) Transcribe(ctx context.Context, wavData []byte, opts TranscribeOptions) (string, error) {
	if len(wavData) == 0 {
		return "", fmt.Errorf("empty WAV data")
	}

	// Build the multipart form body.
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// File field.
	filePart, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", fmt.Errorf("create form file field: %w", err)
	}
	if _, err := filePart.Write(wavData); err != nil {
		return "", fmt.Errorf("write WAV data to form: %w", err)
	}

	// Model field.
	if err := writer.WriteField("model", "whisper-1"); err != nil {
		return "", fmt.Errorf("write model field: %w", err)
	}

	// Response format field.
	if err := writer.WriteField("response_format", "json"); err != nil {
		return "", fmt.Errorf("write response_format field: %w", err)
	}

	// Optional: initial prompt for vocabulary hints.
	if opts.InitialPrompt != "" {
		if err := writer.WriteField("initial_prompt", opts.InitialPrompt); err != nil {
			return "", fmt.Errorf("write initial_prompt field: %w", err)
		}
	}

	// Optional: language hint.
	if opts.Language != "" {
		if err := writer.WriteField("language", opts.Language); err != nil {
			return "", fmt.Errorf("write language field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	// Try OpenAI-compatible endpoint first, fall back to whisper.cpp /inference.
	endpoint := c.resolveEndpoint(ctx)
	contentType := writer.FormDataContentType()
	requestBody := body.Bytes()
	respBody, statusCode, err := c.sendTranscribeRequest(ctx, endpoint, contentType, requestBody)
	if err != nil {
		return "", err
	}
	if statusCode != http.StatusOK && endpoint == c.openAIEndpoint() {
		fallback := c.inferenceEndpoint()
		respBody, statusCode, err = c.sendTranscribeRequest(ctx, fallback, contentType, requestBody)
		if err != nil {
			return "", err
		}
		if statusCode == http.StatusOK {
			c.setEndpoint(fallback)
		}
	}
	if statusCode != http.StatusOK {
		return "", fmt.Errorf("whisper API returned status %d: %s", statusCode, truncate(respBody, 512))
	}

	var result transcriptionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse transcription response: %w (body: %s)", err, truncate(respBody, 256))
	}

	return strings.TrimSpace(result.Text), nil
}

// HealthCheck verifies connectivity to the Whisper server by hitting its health
// endpoint. Returns nil if the server responds with HTTP 200.
func (c *Client) HealthCheck(ctx context.Context) error {
	url := c.whisperURL + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create health check request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()
	// Drain body so the connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}
	return nil
}

// resolveEndpoint detects whether the server supports the OpenAI-compatible
// endpoint or the whisper.cpp /inference endpoint. Caches the result.
func (c *Client) resolveEndpoint(ctx context.Context) string {
	c.mu.RLock()
	if c.endpoint != "" {
		ep := c.endpoint
		c.mu.RUnlock()
		return ep
	}
	c.mu.RUnlock()

	// Probe /v1/audio/transcriptions. If unavailable, use /inference.
	openaiURL := c.openAIEndpoint()
	inferenceURL := c.inferenceEndpoint()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openaiURL, nil)
	if err != nil {
		c.mu.Lock()
		c.endpoint = inferenceURL
		ep := c.endpoint
		c.mu.Unlock()
		return ep
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.mu.Lock()
		c.endpoint = inferenceURL
		ep := c.endpoint
		c.mu.Unlock()
		return ep
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	c.mu.Lock()
	defer c.mu.Unlock()
	if resp.StatusCode == http.StatusNotFound {
		c.endpoint = inferenceURL
	} else {
		c.endpoint = openaiURL
	}
	return c.endpoint
}

// ResetEndpoint clears endpoint autodetection cache. Call this when the
// server restarts or changes implementation while keeping the same base URL.
func (c *Client) ResetEndpoint() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endpoint = ""
}

func (c *Client) sendTranscribeRequest(ctx context.Context, endpoint, contentType string, payload []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("send transcription request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response body: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

func (c *Client) openAIEndpoint() string {
	return c.whisperURL + "/v1/audio/transcriptions"
}

func (c *Client) inferenceEndpoint() string {
	return c.whisperURL + "/inference"
}

func (c *Client) setEndpoint(endpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endpoint = endpoint
}

// truncate returns a string of at most max bytes from b, for use in error messages.
func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}

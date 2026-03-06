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
	"time"
)

const defaultWhisperURL = "http://127.0.0.1:2022"

// Client is an HTTP client for Whisper speech-to-text APIs.
// Supports both OpenAI-compatible (/v1/audio/transcriptions) and whisper.cpp (/inference).
type Client struct {
	whisperURL string
	httpClient *http.Client
	endpoint   string // cached resolved endpoint
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

// Transcribe sends WAV audio data to the Whisper API and returns the transcribed text.
func (c *Client) Transcribe(ctx context.Context, wavData []byte) (string, error) {
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

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	// Try OpenAI-compatible endpoint first, fall back to whisper.cpp /inference.
	endpoint := c.resolveEndpoint(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send transcription request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("whisper API returned status %d: %s", resp.StatusCode, truncate(respBody, 512))
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
	if c.endpoint != "" {
		return c.endpoint
	}

	// Probe /v1/audio/transcriptions with OPTIONS/HEAD — if it 404s, use /inference.
	openaiURL := c.whisperURL + "/v1/audio/transcriptions"
	inferenceURL := c.whisperURL + "/inference"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openaiURL, nil)
	if err != nil {
		c.endpoint = inferenceURL
		return c.endpoint
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.endpoint = inferenceURL
		return c.endpoint
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		c.endpoint = inferenceURL
	} else {
		c.endpoint = openaiURL
	}
	return c.endpoint
}

// truncate returns a string of at most max bytes from b, for use in error messages.
func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}

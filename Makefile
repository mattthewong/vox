.PHONY: build test test-short lint run setup start clean install deps fmt

build:
	go build -o bin/vox ./cmd/vox

test:
	go test -v ./...

test-short:
	go test -short -v ./...

lint:
	go vet ./...
	@if command -v staticcheck >/dev/null 2>&1; then staticcheck ./...; fi

run:
	go run ./cmd/vox

setup:
	@set -eu; \
	if ! command -v brew >/dev/null 2>&1; then \
		echo "Error: Homebrew is required: https://brew.sh"; \
		exit 1; \
	fi; \
	if command -v rec >/dev/null 2>&1 || command -v ffmpeg >/dev/null 2>&1; then \
		echo "Audio recorder already installed (sox or ffmpeg found)"; \
	else \
		echo "Installing sox..."; \
		brew install sox; \
	fi; \
	if command -v whisper-server >/dev/null 2>&1; then \
		echo "whisper-cpp already installed"; \
	else \
		echo "Installing whisper-cpp..."; \
		brew install whisper-cpp; \
	fi; \
	MODEL_DIR="$${WHISPER_MODEL_DIR:-$$HOME/.local/share/whisper-cpp}"; \
	MODEL_PATH="$${WHISPER_MODEL:-$$MODEL_DIR/ggml-base.en.bin}"; \
	mkdir -p "$$MODEL_DIR"; \
	if [ -f "$$MODEL_PATH" ]; then \
		echo "Whisper model already exists at $$MODEL_PATH"; \
	else \
		echo "Downloading whisper model to $$MODEL_PATH..."; \
		curl -L -o "$$MODEL_PATH" "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin"; \
	fi; \
	echo "Setup complete. Run: make start"

start:
	@set -eu; \
	MODEL_PATH="$${WHISPER_MODEL:-$$HOME/.local/share/whisper-cpp/ggml-base.en.bin}"; \
	LOG_PATH="$${WHISPER_LOG:-logs/whisper.log}"; \
	if ! command -v whisper-server >/dev/null 2>&1; then \
		echo "Error: whisper-server not found. Install whisper-cpp."; \
		exit 1; \
	fi; \
	if [ ! -f "$$MODEL_PATH" ]; then \
		echo "Error: Whisper model not found at $$MODEL_PATH"; \
		exit 1; \
	fi; \
	mkdir -p "$$(dirname "$$LOG_PATH")"; \
	whisper-server --host 127.0.0.1 --port 2022 --model "$$MODEL_PATH" >"$$LOG_PATH" 2>&1 & \
	WHISPER_PID=$$!; \
	cleanup() { \
		kill "$$WHISPER_PID" 2>/dev/null || true; \
		wait "$$WHISPER_PID" 2>/dev/null || true; \
	}; \
	trap cleanup EXIT INT TERM; \
	echo "✅ Whisper http://127.0.0.1:2022 (log: $$LOG_PATH)"; \
	go run ./cmd/vox

clean:
	rm -rf bin/

install:
	go build -o /usr/local/bin/vox ./cmd/vox

deps:
	@if command -v rec >/dev/null 2>&1; then \
		echo "sox already installed"; \
	else \
		echo "Installing sox..."; \
		brew install sox; \
	fi

fmt:
	gofmt -s -w .

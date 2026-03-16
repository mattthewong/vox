.PHONY: build test test-short lint run setup clean install deps fmt

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

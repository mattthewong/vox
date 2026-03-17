.PHONY: build test test-short lint run start clean install deps fmt

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

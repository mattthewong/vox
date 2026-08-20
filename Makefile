.PHONY: test-parakeet build app test test-short test-race lint run setup start stop status clean install deps fmt ci check-fmt

export CGO_LDFLAGS := -Wl,-no_warn_duplicate_libraries

# Bundle identity. Stable so macOS TCC keeps perms across rebuilds.
APP_BUNDLE    := bin/Vox.app
APP_BUNDLE_ID := dev.vox.menubar

build:
	go build -o bin/vox ./cmd/vox

# app wraps bin/vox in a real .app bundle so TCC keys perms on bundle ID.
app: build
	@rm -rf $(APP_BUNDLE)
	@mkdir -p $(APP_BUNDLE)/Contents/MacOS
	@cp packaging/Info.plist $(APP_BUNDLE)/Contents/Info.plist
	@cp bin/vox $(APP_BUNDLE)/Contents/MacOS/vox
	@./packaging/bundle-dylibs.sh $(APP_BUNDLE) $(APP_BUNDLE_ID) >/dev/null
	@echo "Built $(APP_BUNDLE) ($(APP_BUNDLE_ID))"

test:
	go test -v ./...

test-short:
	go test -short -v ./...

test-parakeet:
	go test -tags parakeet_integration -v ./internal/parakeet/

test-race:
	go test -race -short -v ./...

lint:
	go vet ./...
	@if command -v staticcheck >/dev/null 2>&1; then staticcheck ./...; fi

run:
	go run ./cmd/vox

# setup installs the system dependencies (sox/ffmpeg, whisper-cpp, model
# file). Idempotent and quiet when everything is already in place, so it's
# safe to depend on from `start`. Permissions (Accessibility, Microphone)
# are intentionally handled by Vox.app's first launch, not here — that way
# TCC only prompts once, for the bundle identity, not twice (bare binary + bundle).
setup:
	@set -eu; \
	if ! command -v brew >/dev/null 2>&1; then \
		echo "Error: Homebrew is required: https://brew.sh"; \
		exit 1; \
	fi; \
	if ! command -v go >/dev/null 2>&1; then \
		echo "📦 Installing go..."; \
		brew install go; \
	fi; \
	if ! command -v rec >/dev/null 2>&1 && ! command -v ffmpeg >/dev/null 2>&1; then \
		echo "📦 Installing sox..."; \
		brew install sox; \
	fi; \
	if ! command -v whisper-server >/dev/null 2>&1; then \
		echo "📦 Installing whisper-cpp..."; \
		brew install whisper-cpp; \
	fi; \
	MODEL_DIR="$${WHISPER_MODEL_DIR:-$$HOME/.local/share/whisper-cpp}"; \
	MODEL_PATH="$${WHISPER_MODEL:-$$MODEL_DIR/ggml-base.en.bin}"; \
	if [ ! -f "$$MODEL_PATH" ]; then \
		echo "⬇️  Downloading whisper model (~150MB) to $$MODEL_PATH..."; \
		mkdir -p "$$MODEL_DIR"; \
		curl --fail --proto '=https' --tlsv1.2 -L -o "$$MODEL_PATH" \
			"https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin"; \
		echo "a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002  $$MODEL_PATH" \
			| shasum -a 256 -c - >/dev/null || { \
				echo "❌ Model checksum mismatch — deleting $$MODEL_PATH"; \
				rm -f "$$MODEL_PATH"; \
				exit 1; \
			}; \
	fi

# start is the one-command entry point: ensures system deps (`setup`), builds
# the .app bundle (`app`), and launches Vox detached so the terminal can
# close. Vox manages whisper-server internally when using the default local URL.
start: setup app
	@set -eu; \
	mkdir -p logs; \
	if [ -f logs/vox.pid ] && kill -0 "$$(cat logs/vox.pid)" 2>/dev/null; then \
		echo "Vox already running (pid $$(cat logs/vox.pid)). Run: make stop"; exit 1; \
	fi; \
	VOX_LOG_PATH="$$(pwd)/logs/vox.log" \
	VOX_PID_PATH="$$(pwd)/logs/vox.pid" \
		nohup "$(APP_BUNDLE)/Contents/MacOS/vox" >logs/vox.log 2>&1 & \
	echo $$! > logs/vox.pid; \
	echo "✅ Vox running (pid $$!) — look for it in the menubar (logs/vox.log)"

stop:
	@pid="$$(cat logs/vox.pid 2>/dev/null)" || true; \
	if [ -n "$$pid" ] && ps -p "$$pid" -o comm= 2>/dev/null | grep -q "vox" && kill "$$pid" 2>/dev/null; then \
		echo "stopped vox (pid $$pid)"; \
	fi; \
	rm -f logs/vox.pid logs/vox.log logs/whisper.log

status:
	@pid="$$(cat logs/vox.pid 2>/dev/null)" || true; \
	if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
		echo "vox: running (pid $$pid)"; \
	else \
		echo "vox: not running"; \
	fi

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

check-fmt:
	@unformatted=$$(gofmt -s -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Error: unformatted files:"; \
		echo "$$unformatted"; \
		echo "Run: make fmt"; \
		exit 1; \
	fi

ci: build lint check-fmt test-race
	@echo "All CI checks passed."

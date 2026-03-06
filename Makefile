.PHONY: build test test-short lint run clean install deps fmt

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

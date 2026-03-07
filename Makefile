.PHONY: build build-all release snapshot clean install-deps install-zig

# Build for current platform only
build:
	go build -o gonfig .

# Build all platforms using Zig
build-all:
	goreleaser build --snapshot --clean --skip=validate

# Create a release using Zig
release:
	goreleaser release --clean

# Build snapshot using Zig
snapshot:
	goreleaser build --snapshot --clean

# Clean build artifacts
clean:
	rm -rf dist/
	rm -f gonfig

# Install Zig (required for cross-compilation with CGO)
install-deps: install-zig

# Install Zig from GitHub
install-zig:
	@echo "Installing Zig..."
	@if command -v zig >/dev/null 2>&1; then \
		echo "Zig is already installed: $$(zig version)"; \
	else \
		echo "Please install Zig manually from https://ziglang.org/download/"; \
		echo "Or use: go install github.com/dosgo/zigtool/zig@latest"; \
	fi

# Test
test:
	go test ./...

# Install goreleaser if not present
install-goreleaser:
	@if ! command -v goreleaser >/dev/null 2>&1; then \
		echo "Installing goreleaser..."; \
		go install github.com/goreleaser/goreleaser/v2@latest; \
	else \
		echo "goreleaser is already installed: $$(goreleaser --version)"; \
	fi

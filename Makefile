.PHONY: build build-all release prepare-release-branch release-draft release-dry-run snapshot clean install-deps install-zig install-goreleaser test check-clean check-main-up-to-date check-version-format check-melange-version

MELANGE_VERSION := $(strip $(shell awk '/^  version:/ {print $$2; exit}' melange.yaml))
VERSION ?= v$(MELANGE_VERSION)

# Build for current platform only
build:
	go build -o gonfig .

# Build all platforms using Zig
build-all:
	goreleaser build --snapshot --clean --skip=validate

# Create a release using Zig
release:
	@echo "Publishing is handled by GitHub Actions."
	@echo "Use: make release-draft"
	@echo "Optional override: make release-draft VERSION=vX.Y.Z"
	@echo "Then monitor workflows: .github/workflows/release.yml and .github/workflows/melange.yml"

# Create a release preparation branch from main, update melange.yaml version,
# commit the change, and push the branch to origin.
# Usage: make prepare-release-branch VERSION=v1.2.3 [RELEASE_BRANCH=release/v1.2.3]
prepare-release-branch: check-main-up-to-date check-clean check-version-format
	@branch_name="$(RELEASE_BRANCH)"; \
	if [ -z "$$branch_name" ]; then \
		branch_name="release/$(VERSION)"; \
	fi; \
	if git rev-parse --verify "$$branch_name" >/dev/null 2>&1; then \
		echo "Local branch '$$branch_name' already exists."; \
		exit 1; \
	fi; \
	if git ls-remote --heads origin "$$branch_name" | grep -q "$$branch_name"; then \
		echo "Remote branch '$$branch_name' already exists on origin."; \
		exit 1; \
	fi; \
	expected="$${VERSION#v}"; \
	current="$$(awk '/^  version:/ {print $$2; exit}' melange.yaml)"; \
	if [ -z "$$current" ]; then \
		echo "Could not read package version from melange.yaml"; \
		exit 1; \
	fi; \
	if [ "$$current" = "$$expected" ]; then \
		echo "melange.yaml already has version $$expected. Nothing to prepare."; \
		exit 1; \
	fi; \
	echo "Creating branch $$branch_name from main..."; \
	git switch -c "$$branch_name"; \
	awk -v ver="$$expected" 'BEGIN{updated=0} /^  version:/ && !updated {print "  version: " ver; updated=1; next} {print} END{if (!updated) exit 2}' melange.yaml > melange.yaml.tmp; \
	if [ $$? -ne 0 ]; then \
		echo "Failed to update melange.yaml version line."; \
		rm -f melange.yaml.tmp; \
		exit 1; \
	fi; \
	mv melange.yaml.tmp melange.yaml; \
	git add melange.yaml; \
	git commit -m "chore(release): prepare $(VERSION)"; \
	git push -u origin "$$branch_name"; \
	echo "Release prep branch pushed: $$branch_name"

# Create and push a git tag for a release.
# Build and publishing are handled by GitHub Actions workflows.
# Usage: make release-draft
# Optional: make release-draft VERSION=v1.2.3
release-draft: check-main-up-to-date check-clean check-version-format check-melange-version test
	@if git rev-parse "$(VERSION)" >/dev/null 2>&1; then \
		echo "Tag $(VERSION) already exists"; \
		exit 1; \
	fi
	@if git ls-remote --tags origin "refs/tags/$(VERSION)" | grep -q "$(VERSION)"; then \
		echo "Tag $(VERSION) already exists on origin"; \
		exit 1; \
	fi
	@echo "Creating tag $(VERSION)..."
	@git tag -a "$(VERSION)" -m "Release $(VERSION)"
	@echo "Pushing tag $(VERSION)..."
	@git push origin "$(VERSION)"
	@echo "Release workflows triggered by tag $(VERSION)."
	@echo "Track progress in GitHub Actions: release.yml and melange.yml"

# Run goreleaser in local snapshot mode as a release dry-run
release-dry-run: install-goreleaser
	goreleaser release --snapshot --clean

# Build snapshot using Zig
snapshot:
	goreleaser build --snapshot --clean

# Clean build artifacts
clean:
	rm -rf dist/
	rm -f gonfig

# Ensure no local changes are pending before cutting a release
check-clean:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Working tree is not clean. Commit or stash changes before releasing."; \
		exit 1; \
	fi

# Ensure release is drafted from the latest commit on main
check-main-up-to-date:
	@current_branch="$$(git rev-parse --abbrev-ref HEAD)"; \
	if [ "$$current_branch" != "main" ]; then \
		echo "Current branch is '$$current_branch'. Switch to 'main' before releasing."; \
		exit 1; \
	fi
	@git fetch origin main --quiet
	@local_main="$$(git rev-parse HEAD)"; \
	remote_main="$$(git rev-parse origin/main)"; \
	if [ "$$local_main" != "$$remote_main" ]; then \
		echo "Local main is not up to date with origin/main."; \
		echo "Run: git pull --ff-only origin main"; \
		exit 1; \
	fi

# Ensure VERSION is provided and follows semantic tag format vX.Y.Z
check-version-format:
	@if ! echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$'; then \
		echo "VERSION must match semantic version format: vX.Y.Z"; \
		echo "Detected VERSION='$(VERSION)'"; \
		echo "Set package.version in melange.yaml or override with VERSION=vX.Y.Z"; \
		exit 1; \
	fi

# Ensure melange package version matches the requested release tag (without leading v)
check-melange-version:
	@if [ -z "$(VERSION)" ]; then \
		echo "VERSION is required. Example: make release-draft VERSION=v1.2.3"; \
		exit 1; \
	fi
	@expected="$${VERSION#v}"; \
	melange_version="$$(awk '/^  version:/ {print $$2; exit}' melange.yaml)"; \
	if [ -z "$$melange_version" ]; then \
		echo "Could not read package version from melange.yaml"; \
		exit 1; \
	fi; \
	if [ "$$melange_version" != "$$expected" ]; then \
		echo "melange.yaml version ($$melange_version) does not match VERSION ($$expected)"; \
		echo "Run: make prepare-release-branch VERSION=$(VERSION)"; \
		echo "Open and merge the PR into main, then run release-draft from main."; \
		exit 1; \
	fi

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

#!/usr/bin/env bash
#
# Build Yaria CLI binaries for npm distribution.
# Run from the YariaPlus root directory.
#
# Usage: ./npm/build-npm.sh
#
# Binaries are placed in npm/dist/ and should be uploaded to
# yaria.live/releases/ for the npm postinstall script to fetch.
#

set -euo pipefail

cd "$(dirname "$0")/.."
PROJECT_DIR="$(pwd)"
DIST_DIR="${PROJECT_DIR}/npm/dist"

# Read version from main.go
CLI_VERSION=$(grep 'const version' cmd/yaria/main.go | sed 's/.*= "//;s/".*//')
NPM_VERSION=$(grep '"version"' npm/package.json | head -1 | sed 's/.*: "//;s/".*//')

echo "Building Yaria CLI binaries..."
echo "  CLI version: $CLI_VERSION"
echo "  npm version: $NPM_VERSION"

if [ "$CLI_VERSION" != "$NPM_VERSION" ]; then
    echo ""
    echo "  WARNING: Version mismatch!"
    echo "  Update cmd/yaria/main.go or npm/package.json to match."
    echo ""
fi

mkdir -p "$DIST_DIR"

# Build with pro tag so users can activate Mantorex with a license key
BUILD_TAGS="-tags pro"

# Linux amd64
echo "  linux-amd64..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $BUILD_TAGS -ldflags="-s -w" -o "$DIST_DIR/yaria-linux-amd64" ./cmd/yaria/
echo "  done"

# Linux arm64
echo "  linux-arm64..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $BUILD_TAGS -ldflags="-s -w" -o "$DIST_DIR/yaria-linux-arm64" ./cmd/yaria/
echo "  done"

# macOS amd64
echo "  darwin-amd64..."
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $BUILD_TAGS -ldflags="-s -w" -o "$DIST_DIR/yaria-darwin-amd64" ./cmd/yaria/
echo "  done"

# macOS arm64
echo "  darwin-arm64..."
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $BUILD_TAGS -ldflags="-s -w" -o "$DIST_DIR/yaria-darwin-arm64" ./cmd/yaria/
echo "  done"

# Windows amd64
echo "  windows-amd64..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $BUILD_TAGS -ldflags="-s -w" -o "$DIST_DIR/yaria-windows-amd64.exe" ./cmd/yaria/
echo "  done"

echo ""
echo "Binaries:"
ls -lh "$DIST_DIR/"
echo ""
echo "Upload to yaria.live/releases/"
echo ""
echo "To publish to npm:"
echo "  cd npm/"
echo "  npm publish"

#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
go test -race ./...
go vet ./...
# SDK/LLVM resources are optional for portable CI, required for release builds.
if command -v llvm-rc >/dev/null 2>&1 || [ -x /opt/homebrew/opt/llvm/bin/llvm-rc ]; then
    go run ./scripts/resources
else
    go run ./scripts/resources -assets-only
fi
# Compile every Windows-only package, without claiming Windows runtime coverage.
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...

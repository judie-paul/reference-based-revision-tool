#!/bin/sh
# Builds the revision tool into the build/ folder (Linux / macOS / Git Bash).
#   ./build.sh        -> build/revisiontool[.exe]    (current OS/architecture)
#   ./build.sh all    -> also build/revisiontool.exe and build/revisiontool-linux-amd64
set -eu

root=$(cd "$(dirname "$0")" && pwd)
out="$root/build"
mkdir -p "$out"
cd "$root/code"

export CGO_ENABLED=0   # pure Go: no C compiler needed

echo "Running tests..."
go test ./...

exe=$(go env GOEXE)
echo "Building build/revisiontool$exe ..."
go build -trimpath -ldflags "-s -w" -o "$out/revisiontool$exe" ./cmd/revisiontool

if [ "${1:-}" = "all" ]; then
    echo "Building build/revisiontool.exe (Windows amd64) ..."
    GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "$out/revisiontool.exe" ./cmd/revisiontool
    echo "Building build/revisiontool-linux-amd64 ..."
    GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "$out/revisiontool-linux-amd64" ./cmd/revisiontool
fi

echo "Done. Binaries are in $out"

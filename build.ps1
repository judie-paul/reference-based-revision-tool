# Builds the revision tool into the build\ folder (Windows PowerShell).
#   .\build.ps1          -> build\revisiontool.exe            (Windows, amd64)
#   .\build.ps1 -All     -> also build\revisiontool-linux-amd64 (Linux, amd64)
param([switch]$All)
$ErrorActionPreference = "Stop"

$root = $PSScriptRoot
$out = Join-Path $root "build"
New-Item -ItemType Directory -Force $out | Out-Null

Push-Location (Join-Path $root "code")
try {
    $env:CGO_ENABLED = "0"   # pure Go: no C compiler needed

    Write-Host "Running tests..."
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw "tests failed" }

    Write-Host "Building build\revisiontool.exe ..."
    $env:GOOS = "windows"; $env:GOARCH = "amd64"
    go build -trimpath -ldflags "-s -w" -o (Join-Path $out "revisiontool.exe") ./cmd/revisiontool
    if ($LASTEXITCODE -ne 0) { throw "windows build failed" }

    if ($All) {
        Write-Host "Building build\revisiontool-linux-amd64 ..."
        $env:GOOS = "linux"; $env:GOARCH = "amd64"
        go build -trimpath -ldflags "-s -w" -o (Join-Path $out "revisiontool-linux-amd64") ./cmd/revisiontool
        if ($LASTEXITCODE -ne 0) { throw "linux build failed" }
    }
}
finally {
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Pop-Location
}
Write-Host "Done. Binaries are in $out"

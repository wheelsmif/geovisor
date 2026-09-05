param(
    [string]$Version = "0.0.0-dev"
)

$ErrorActionPreference = "Stop"
if ($Version -notmatch '^(0\.0\.0-dev|v?\d+\.\d+\.\d+([.-][0-9A-Za-z.-]+)?)$') {
    throw "version must be 0.0.0-dev or a semantic version"
}

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
$stageRoot = Join-Path $dist ".stage"
$toolchain = (Get-Content (Join-Path $root ".go-version") -Raw).Trim()
$targets = @(
    @("windows", "amd64"), @("windows", "arm64"),
    @("linux", "amd64"), @("linux", "arm64"),
    @("darwin", "amd64"), @("darwin", "arm64")
)

Remove-Item -Recurse -Force $dist -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $stageRoot | Out-Null

$oldGoos, $oldGoarch, $oldCgo, $oldToolchain = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED, $env:GOTOOLCHAIN
try {
    Push-Location $root
    $env:CGO_ENABLED = "0"
    $env:GOTOOLCHAIN = "go$toolchain"
    $ldflags = "-s -w -buildid= -X github.com/wheelsmif/geovisor/internal/version.Version=$Version"

    foreach ($target in $targets) {
        $os, $arch = $target
        $env:GOOS, $env:GOARCH = $os, $arch
        $suffix = if ($os -eq "windows") { ".exe" } else { "" }
        $stage = Join-Path $stageRoot "$os-$arch"
        New-Item -ItemType Directory -Force -Path $stage | Out-Null

        go build -trimpath -buildvcs=false -ldflags $ldflags -o (Join-Path $stage "geovisor$suffix") ./cmd/geovisor
        go build -trimpath -buildvcs=false -ldflags $ldflags -o (Join-Path $stage "gv$suffix") ./cmd/geovisor
        $metadata = go version -m (Join-Path $stage "geovisor$suffix") | Out-String
        if ($metadata -notmatch "github.com/wheelsmif/geovisor" -or
            $metadata -notmatch "CGO_ENABLED=0") {
            throw "invalid Go build metadata for $os/$arch"
        }
        Copy-Item LICENSE, README.md, SECURITY.md, CONTRIBUTING.md -Destination $stage

        $archive = Join-Path $dist "geovisor_${Version}_${os}_${arch}.tar.gz"
        tar.exe -czf $archive -C $stage .
        if ($LASTEXITCODE -ne 0) { throw "tar failed for $os/$arch" }

        if ($os -eq "windows" -and $arch -eq "amd64" -and
            $env:PROCESSOR_ARCHITECTURE -eq "AMD64") {
            $binary = Join-Path $stage "geovisor.exe"
            $process = New-Object System.Diagnostics.Process
            $process.StartInfo.FileName = $binary
            $process.StartInfo.Arguments = "--version"
            $process.StartInfo.UseShellExecute = $false
            $process.StartInfo.RedirectStandardError = $true
            $null = $process.Start()
            $actual = $process.StandardError.ReadToEnd().Trim()
            $process.WaitForExit()
            if ($actual -ne $Version) {
                throw "Windows version output was '$actual', want '$Version'"
            }
        }
    }

    Get-ChildItem $dist -Filter *.tar.gz |
        Sort-Object Name |
        ForEach-Object {
            $hash = (Get-FileHash -Algorithm SHA256 $_.FullName).Hash.ToLowerInvariant()
            "$hash  $($_.Name)"
        } |
        Set-Content -Encoding ascii (Join-Path $dist "checksums.txt")
}
finally {
    Pop-Location
    $env:GOOS, $env:GOARCH, $env:CGO_ENABLED, $env:GOTOOLCHAIN = $oldGoos, $oldGoarch, $oldCgo, $oldToolchain
    Remove-Item -Recurse -Force $stageRoot -ErrorAction SilentlyContinue
}

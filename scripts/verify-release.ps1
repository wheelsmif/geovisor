$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$work = Join-Path ([System.IO.Path]::GetTempPath()) "geovisor-release-verify-$PID"
New-Item -ItemType Directory -Force -Path $work | Out-Null
$oldToolchain = $env:GOTOOLCHAIN

try {
    Push-Location $root
    $env:GOTOOLCHAIN = "go$((Get-Content .go-version -Raw).Trim())"
    $version = "v0.0.0-test"
    $ldflags = "-buildid= -X github.com/geo-suite/geovisor/internal/version.Version=$version"

    go build -trimpath -buildvcs=false -ldflags $ldflags -o (Join-Path $work "geovisor-a.exe") ./cmd/geovisor
    go build -trimpath -buildvcs=false -ldflags $ldflags -o (Join-Path $work "geovisor-b.exe") ./cmd/geovisor
    go build -trimpath -buildvcs=false -ldflags $ldflags -o (Join-Path $work "gv.exe") ./cmd/geovisor

    $first = (Get-FileHash -Algorithm SHA256 (Join-Path $work "geovisor-a.exe")).Hash
    $second = (Get-FileHash -Algorithm SHA256 (Join-Path $work "geovisor-b.exe")).Hash
    if ($first -ne $second) {
        throw "repeated deterministic builds produced different binaries"
    }

    $geovisorPath = Join-Path $work "geovisor-a.exe"
    $gvPath = Join-Path $work "gv.exe"
    function Read-Version([string]$Path) {
        $process = New-Object System.Diagnostics.Process
        $process.StartInfo.FileName = $Path
        $process.StartInfo.Arguments = "--version"
        $process.StartInfo.UseShellExecute = $false
        $process.StartInfo.RedirectStandardError = $true
        $null = $process.Start()
        $value = $process.StandardError.ReadToEnd().Trim()
        $process.WaitForExit()
        if ($pro
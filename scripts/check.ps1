param(
    [switch]$RequireBrowser,
    [switch]$Security
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    $env:GOTOOLCHAIN = "go$((Get-Content .go-version -Raw).Trim())"
    if ($RequireBrowser) { $env:GEOVISOR_REQUIRE_BROWSER = "1" }

    npm ci
    npm run check
    $unformatted = gofmt -l .
    if ($unformatted) { throw "gofmt required:`n$($unformatted -join "`n")" }
    go vet ./...
    go test ./...
    ./scripts/verify-release.ps1

    if ($Security) {
        npm audit --audit-level=high
        go run golang.org/x/vuln/cmd/govulncheck@latest ./...
    }
}
finally {
    Pop-Location
}

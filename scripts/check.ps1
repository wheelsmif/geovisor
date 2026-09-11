param(
    [switch]$RequireBrowser,
    [switch]$Security
)

$ErrorActionPreference = "Stop"

# Windows PowerShell does not surface native command exit codes through
# $ErrorActionPreference, so every external step is checked explicitly.
function Invoke-Step {
    param([Parameter(Mandatory)][scriptblock]$Step)
    & $Step
    if ($LASTEXITCODE -ne 0) { throw "failed ($LASTEXITCODE): $Step" }
}

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    $env:GOTOOLCHAIN = "go$((Get-Content .go-version -Raw).Trim())"
    if ($RequireBrowser) { $env:GEOVISOR_REQUIRE_BROWSER = "1" }

    Invoke-Step { npm ci }
    Invoke-Step { npm run check }
    $unformatted = gofmt -l .
    if ($unformatted) { throw "gofmt required:`n$($unformatted -join "`n")" }
    Invoke-Step { go vet ./... }
    Invoke-Step { go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./... }
    Invoke-Step { go test ./... }
    Invoke-Step { ./scripts/verify-release.ps1 }

    if ($Security) {
        Invoke-Step { npm audit --audit-level=high }
        Invoke-Step { go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./... }
    }
}
finally {
    Pop-Location
}

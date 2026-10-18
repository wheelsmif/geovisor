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
$oldToolchain = $env:GOTOOLCHAIN
$oldRequireBrowser = $env:GEOVISOR_REQUIRE_BROWSER
$oldRequireNode = $env:GEOVISOR_REQUIRE_NODE
Push-Location $root
try {
    $env:GOTOOLCHAIN = "go$((Get-Content .go-version -Raw).Trim())"
    if ($RequireBrowser) { $env:GEOVISOR_REQUIRE_BROWSER = "1" }

    Invoke-Step { npm ci }
    Invoke-Step { npm run check }
    # npm succeeded, so Node is present. Fail the apply harness instead of skipping.
    $env:GEOVISOR_REQUIRE_NODE = "1"
    $unformatted = @(gofmt -l .)
    if ($LASTEXITCODE -ne 0) { throw "gofmt failed ($LASTEXITCODE)" }
    if ($unformatted) { throw "gofmt required:`n$($unformatted -join "`n")" }
    Invoke-Step { go vet ./... }
    Invoke-Step { go tool "-modfile=tools/go.mod" staticcheck ./... }
    Invoke-Step { go test ./... }
    Invoke-Step { ./scripts/verify-release.ps1 }

    if ($Security) {
        Invoke-Step { npm audit --audit-level=high }
        Invoke-Step { go tool "-modfile=tools/go.mod" govulncheck ./... }
    }
}
finally {
    Pop-Location
    $env:GOTOOLCHAIN = $oldToolchain
    $env:GEOVISOR_REQUIRE_BROWSER = $oldRequireBrowser
    $env:GEOVISOR_REQUIRE_NODE = $oldRequireNode
}

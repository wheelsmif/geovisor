# Windows-only release build. Archive creation calls tar.exe, the bsdtar
# shipped with Windows. scripts/build-release.sh is the Unix entry point.
#
# Binaries are compiled from a git archive of HEAD so the compiler reads the
# LF blobs stored in git, not a working tree checked out with core.autocrlf.
# Uncommitted edits are absent from that archive.
#
# tar.exe does not accept GNU tar's --sort, --mtime, --owner, or --group.
# When those flags are missing, members are added in sorted order, staged
# file times are set to the Unix epoch, and uid/gid are set with the bsdtar
# flags --uid=0 --gid=0 --numeric-owner when tar.exe accepts them. gzip -n is
# used when gzip is on PATH. Otherwise tar.exe writes the gzip wrapper and
# this script clears the 4-byte gzip header mtime, which is the timestamp
# gzip -n would have omitted.

param(
    [string]$Version = "0.0.0-dev"
)

$ErrorActionPreference = "Stop"

function Invoke-Step {
    param([Parameter(Mandatory)][scriptblock]$Step)
    & $Step
    if ($LASTEXITCODE -ne 0) { throw "failed ($LASTEXITCODE): $Step" }
}

function Test-TarCreate([string[]]$Extra) {
    $work = Join-Path ([System.IO.Path]::GetTempPath()) ("geovisor-tar-probe-" + [guid]::NewGuid().ToString("n"))
    New-Item -ItemType Directory -Force -Path $work | Out-Null
    try {
        [System.IO.File]::WriteAllText((Join-Path $work "f"), "x")
        $out = Join-Path $work "o.tar"
        & tar.exe @Extra -cf $out -C $work f
        return ($LASTEXITCODE -eq 0 -and (Test-Path -LiteralPath $out))
    }
    finally {
        Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
    }
}

function Clear-GzipHeaderTime([string]$Path) {
    $bytes = [System.IO.File]::ReadAllBytes($Path)
    if ($bytes.Length -lt 10 -or $bytes[0] -ne 0x1f -or $bytes[1] -ne 0x8b) {
        throw "not a gzip archive: $Path"
    }
    $bytes[4] = 0
    $bytes[5] = 0
    $bytes[6] = 0
    $bytes[7] = 0
    [System.IO.File]::WriteAllBytes($Path, $bytes)
}

function Read-Version([string]$Path) {
    $process = New-Object System.Diagnostics.Process
    $process.StartInfo.FileName = $Path
    $process.StartInfo.Arguments = "--version"
    $process.StartInfo.UseShellExecute = $false
    $process.StartInfo.RedirectStandardError = $true
    $null = $process.Start()
    $value = $process.StandardError.ReadToEnd().Trim()
    $process.WaitForExit()
    if ($process.ExitCode -ne 0) { throw "$Path --version failed ($($process.ExitCode))" }
    return $value
}

if ($Version -notmatch '^(0\.0\.0-dev|v?\d+\.\d+\.\d+([.-][0-9A-Za-z.-]+)?)$') {
    throw "version must be 0.0.0-dev or a semantic version"
}

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
$stageRoot = Join-Path $dist ".stage"
$export = Join-Path ([System.IO.Path]::GetTempPath()) "geovisor-release-src-$PID"
$archiveTar = Join-Path ([System.IO.Path]::GetTempPath()) "geovisor-release-src-$PID.tar"
$targets = @(
    @("windows", "amd64"), @("windows", "arm64"),
    @("linux", "amd64"), @("linux", "arm64"),
    @("darwin", "amd64"), @("darwin", "arm64")
)
$epoch = New-Object DateTime 1970, 1, 1, 0, 0, 0, ([DateTimeKind]::Utc)

Remove-Item -Recurse -Force $export, $archiveTar -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $export | Out-Null

$oldGoos, $oldGoarch, $oldCgo, $oldToolchain = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED, $env:GOTOOLCHAIN
$pushed = $false
try {
    Invoke-Step { git -c core.autocrlf=false -C $root archive --format=tar -o $archiveTar HEAD }
    Invoke-Step { tar.exe -xf $archiveTar -C $export }
    Remove-Item -Force $archiveTar
    # Notices are packaged, not compiled. Overlay the working-tree copy so a
    # release build includes the file before it is committed, and strip CR so
    # a core.autocrlf checkout cannot put CRLF into the archive.
    $noticesSrc = Join-Path $root "THIRD_PARTY_NOTICES"
    if (Test-Path -LiteralPath $noticesSrc) {
        $notices = [IO.File]::ReadAllText($noticesSrc) -replace "`r`n", "`n" -replace "`r", "`n"
        $utf8 = New-Object System.Text.UTF8Encoding $false
        [IO.File]::WriteAllText((Join-Path $export "THIRD_PARTY_NOTICES"), $notices, $utf8)
    }

    $gnuTar = @("--sort=name", "--mtime=@0", "--owner=0", "--group=0", "--numeric-owner")
    $uidTar = @("--uid=0", "--gid=0", "--numeric-owner")
    $useGnu = (Test-TarCreate $gnuTar) -and [bool](Get-Command gzip -ErrorAction SilentlyContinue)
    $useUid = -not $useGnu -and (Test-TarCreate $uidTar)

    Remove-Item -Recurse -Force $dist -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force -Path $stageRoot | Out-Null
    Push-Location $export
    $pushed = $true

    $toolchain = (Get-Content (Join-Path $export ".go-version") -Raw).Trim()
    $env:CGO_ENABLED = "0"
    $env:GOTOOLCHAIN = "go$toolchain"
    $ldflags = "-s -w -buildid= -X github.com/wheelsmif/geovisor/internal/version.Version=$Version"

    foreach ($target in $targets) {
        $os, $arch = $target
        $env:GOOS, $env:GOARCH = $os, $arch
        $suffix = if ($os -eq "windows") { ".exe" } else { "" }
        $stage = Join-Path $stageRoot "$os-$arch"
        New-Item -ItemType Directory -Force -Path $stage | Out-Null
        $geovisor = Join-Path $stage "geovisor$suffix"
        $gv = Join-Path $stage "gv$suffix"

        Invoke-Step { go build -trimpath -buildvcs=false -ldflags $ldflags -o $geovisor ./cmd/geovisor }
        Invoke-Step { go build -trimpath -buildvcs=false -ldflags $ldflags -o $gv ./cmd/geovisor }
        if (-not (Test-Path -LiteralPath $gv)) { throw "gv did not build for $os/$arch" }

        $metadata = (Invoke-Step { go version -m $geovisor }) -join "`n"
        if ($metadata -notmatch "github.com/wheelsmif/geovisor" -or
            $metadata -notmatch "CGO_ENABLED=0") {
            throw "invalid Go build metadata for $os/$arch"
        }
        Copy-Item LICENSE, README.md, SECURITY.md, CONTRIBUTING.md, THIRD_PARTY_NOTICES -Destination $stage
        Get-ChildItem -LiteralPath $stage -File | ForEach-Object { $_.LastWriteTimeUtc = $epoch }

        $archive = Join-Path $dist "geovisor_${Version}_${os}_${arch}.tar.gz"
        if ($useGnu) {
            $raw = "$archive.raw"
            Invoke-Step { tar.exe --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner -cf $raw -C $stage . }
            $gzip = (Get-Command gzip).Source
            $gzipErr = "$archive.gzip-err"
            $gzipProc = Start-Process -FilePath $gzip -ArgumentList @("-n", "-c", $raw) -Wait -NoNewWindow -PassThru -RedirectStandardOutput $archive -RedirectStandardError $gzipErr
            $gzipMessage = if (Test-Path -LiteralPath $gzipErr) { (Get-Content -Raw -LiteralPath $gzipErr) } else { "" }
            Remove-Item -Force $raw, $gzipErr -ErrorAction SilentlyContinue
            if ($gzipProc.ExitCode -ne 0) { throw "gzip failed ($($gzipProc.ExitCode)): $gzipMessage" }
        }
        else {
            $names = @(Get-ChildItem -LiteralPath $stage -File | ForEach-Object { $_.Name })
            [Array]::Sort($names, [StringComparer]::Ordinal)
            if ($useUid) {
                Invoke-Step { tar.exe --uid=0 --gid=0 --numeric-owner -czf $archive -C $stage @names }
            }
            else {
                Invoke-Step { tar.exe -czf $archive -C $stage @names }
            }
            Clear-GzipHeaderTime $archive
        }

        if ($os -eq "windows" -and $arch -eq "amd64" -and
            $env:PROCESSOR_ARCHITECTURE -eq "AMD64") {
            foreach ($binary in @($geovisor, $gv)) {
                $actual = Read-Version $binary
                if ($actual -ne $Version) {
                    throw "Windows version output was '$actual', want '$Version'"
                }
            }
        }
    }

    $checksumLines = foreach ($file in @(Get-ChildItem -LiteralPath $dist -Filter *.tar.gz)) { $file }
    [Array]::Sort($checksumLines, [System.Comparison[System.IO.FileInfo]]{
            param($left, $right)
            [string]::CompareOrdinal($left.Name, $right.Name)
        })
    $lines = foreach ($file in $checksumLines) {
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $file.FullName).Hash.ToLowerInvariant()
        "{0}  {1}" -f $hash, $file.Name
    }
    $utf8 = New-Object System.Text.UTF8Encoding $false
    [System.IO.File]::WriteAllText((Join-Path $dist "checksums.txt"), (($lines -join "`n") + "`n"), $utf8)
}
finally {
    if ($pushed) { Pop-Location }
    $env:GOOS, $env:GOARCH, $env:CGO_ENABLED, $env:GOTOOLCHAIN = $oldGoos, $oldGoarch, $oldCgo, $oldToolchain
    Remove-Item -Recurse -Force $stageRoot, $export -ErrorAction SilentlyContinue
    Remove-Item -Force $archiveTar -ErrorAction SilentlyContinue
}

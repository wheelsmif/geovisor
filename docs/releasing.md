# Local release builds

Run the local quality gate first:

```powershell
./scripts/check.ps1 -RequireBrowser -Security
```

Then build a versioned local release:

```powershell
./scripts/build-release.ps1 -Version v1.0.0
```

POSIX equivalents are `./scripts/check.sh --require-browser --security` and
`./scripts/build-release.sh v1.0.0`.

The build script uses the pinned Go toolchain and cross-compiles Windows,
Linux, and macOS for amd64 and arm64 with `CGO_ENABLED=0`. It writes six
ignored `dist/*.tar.gz` archives plus `dist/checksums.txt`. Each archive
contains `geovisor`, `gv`, README, SECURITY, CONTRIBUTING, and the Apache-2.0
`LICENSE`; Windows executable names include `.exe`.

Both executable names are compiled from `./cmd/geovisor`. The requested
version is injected through:

```text
-X github.com/geo-suite/geovisor/internal/version.Version=<version>
```

Builds use `-trimpath`, an empty Go build ID, `-buildvcs=false`, and
`CGO_ENABLED=0`. The separate `verify-release` script compares repeated native
binary bytes and checks both aliases' `--version` output. The archive script
executes only the native target's version command; non-native binaries are
validated by successful cross-compilation and SHA-256 checksums.

Release builds never execute Chromium. At runtime, any later `inspect` launch
still follows the source adapter's mandatory `Leakless(false)` configuration;
release scripts must not add a leakless invocation or wrapper.

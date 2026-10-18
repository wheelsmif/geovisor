# Local release builds

There are no git tags and no published GitHub releases. There are no
downloadable release zips. The committed default version is `0.0.0-dev` in
`internal/version`. `go install github.com/wheelsmif/geovisor/cmd/geovisor@latest`
prints the module version from build info when that version is not `(devel)`
or empty. A local development build still prints `0.0.0-dev`.

Local scripts produce archives. That is not a GitHub Release.

Run the local quality gate first:

```powershell
./scripts/check.ps1 -RequireBrowser -Security
```

Then build a versioned local release. `scripts/build-release.ps1` is
Windows-only because archive creation calls `tar.exe`.

```powershell
./scripts/build-release.ps1 -Version v1.0.0
```

POSIX equivalents are `./scripts/check.sh --require-browser --security` and
`./scripts/build-release.sh v1.0.0`.

Both scripts compile from a `git archive` of `HEAD` so embedded files stay
LF. The compiler reads those git blobs, not a working tree checked out with
`core.autocrlf`. Uncommitted edits are absent from that archive. The scripts
then overlay the working-tree `THIRD_PARTY_NOTICES` and strip CR from it.

The build uses the pinned Go toolchain and cross-compiles Windows, Linux, and
macOS for amd64 and arm64 with `CGO_ENABLED=0`. It writes six ignored
`dist/*.tar.gz` archives plus `dist/checksums.txt`. Each archive contains
`geovisor` and `gv` (Windows names include `.exe`), `LICENSE`, `README.md`,
`SECURITY.md`, `CONTRIBUTING.md`, and `THIRD_PARTY_NOTICES`.

Both executable names are compiled from `./cmd/geovisor`. The requested
version is injected through:

```text
-X github.com/wheelsmif/geovisor/internal/version.Version=<version>
```

Builds use `-trimpath`, an empty Go build ID, `-buildvcs=false`, and
`CGO_ENABLED=0`. The separate `verify-release` script compares repeated native
binary bytes and checks both aliases' `--version` output. Locally it runs from
`check.sh` / `check.ps1`. In CI it runs in the Source cross-build job, not on
every Quality OS. The archive script executes only the native target's version
command; non-native binaries are validated by successful cross-compilation and
SHA-256 checksums.

Release builds never execute Chromium. A later `inspect` launch starts
Chromium with `os/exec` in `internal/browser/launch.go` and reads
`DevToolsActivePort`. The module does not import `rod/lib/launcher`.

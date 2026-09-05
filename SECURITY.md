# Security policy

## Supported versions

Security fixes are applied to the latest released version and the current
development branch. Older releases may not receive backports.

## Reporting a vulnerability

Prefer the repository host's private vulnerability-reporting or security
advisory feature when it is available. Do not publish secrets, exploit details,
private URLs, captured page data, or credentials in a public issue.

If no private reporting feature is available, open a
[minimal repository issue](https://github.com/wheelsmif/geovisor/issues) that
asks maintainers for a private reporting channel. Include no sensitive
technical details in that issue. The project does not currently publish a
separate security contact URL or response-time guarantee.

Useful private reports include the affected version, operating system, browser
version, reproducible steps, impact, and whether the issue crosses the
agent-owned browser execution boundary.

## Security model

GEO-Visor observes caller-selected browser pages and writes static artifacts.
It does not host an MCP daemon or call an LLM service. Treat inspected pages and
generated artifacts as untrusted input and potentially sensitive output.

Safe exploration must never submit, navigate, or issue network requests.
Stealth is explicit opt-in and is not an access-control bypass. Inaccessible
frames remain visible as partial coverage. Current field values, password
values, hidden controls, and option value identifiers must never enter TIR or
emitter output.

Owned Chromium launches disable go-rod's leakless helper at runtime. The
transitive module remains in dependency metadata because go-rod imports it.

Run `./scripts/check.sh --security` or
`./scripts/check.ps1 -Security` to execute `govulncheck` for reachable Go
vulnerabilities and `npm audit` across runtime and build dependencies. High and
critical npm advisories fail the check; lower-severity findings are reviewed
with reachability and build-time exposure recorded rather than described as
absent.

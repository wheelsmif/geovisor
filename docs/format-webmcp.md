# WebMCP JavaScript module

Format name: `webmcp`

The generated ES module targets the Chrome 153 WebMCP Imperative API,
documented as updated on `2026-09-01`. It feature-detects
`document.modelContext.registerTool` and throws a clear error when the API is
unavailable. Each standard registration contains `name`, `description`,
`inputSchema`, WebMCP `annotations`, and an executable `execute` callback.

The callback runs in page JavaScript without Playwright. It tries referenced
TIR locator candidates in deterministic order, traversing:

1. same-origin frame path nodes;
2. open shadow-root path nodes;
3. semantic scope, role, and accessible name;
4. CSS fallbacks.

Role resolution, accessible-name computation, and element addressing come from
`client/src/shared/`, the same source the extractor uses. The extractor records
only semantic locators it has already resolved through that shared matcher, so a
recorded semantic locator is one this callback can resolve.

Where several elements share a role and name, the locator carries `nth`, an
index into the match set in document order. Frame path nodes are addressed by
`nth` alone and carry no CSS fallback: no CSS selector expresses "the Nth frame
of this document", and one that counts element siblings instead would resolve to
the wrong frame rather than report that it cannot resolve.

It supports click, fill, select, and check bindings, checks cancellation between
actions, and reports the tool, action index, action kind, and locator failures
when execution fails. Cross-origin frame DOM access and closed shadow roots
cannot be bypassed by page JavaScript; bindings that encounter either boundary
fail explicitly. The emitter honors `Options.Strict` on each tool's `inputSchema` the same way
as the other schema-emitting lanes. It still rejects actions with no locator
candidate, because those cannot be executed in page JavaScript.

WebMCP annotations use the Chrome 153 names. `readOnlyHint` is true only when
all actions have side effect `none`; `consequentialHint` is true for network,
navigation, submission, or unknown effects. `untrustedContentHint` is true
because tool names and descriptions originate in page text and must be treated
as untrusted model input.

Descriptions, names, locators, and enum values are serialized as data, never
as source text. JSON escaping prevents literal `</script>`, U+2028, and U+2029
from breaking or injecting into the module. Runtime arguments are used only at
invocation time; captured page values, credentials, and secrets are never
embedded.

WebMCP remains experimental and subject to browser API changes. The selected
API snapshot is intentionally explicit so a future revision is a deliberate
emitter update.

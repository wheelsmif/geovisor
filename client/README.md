# Browser payloads

`npm run build` produces two committed bundles from this source tree:

| Entry point | Bundle | Embedded by |
| --- | --- | --- |
| `client/src/extractor.ts` | `internal/payload/extractor.js` | `internal/payload` |
| `client/src/webmcp-runtime.ts` | `internal/emitter/webmcp-runtime.js` | `internal/emitter` |

They share `client/src/shared/`, which owns role resolution, accessible-name
computation, `<select>` option labels, and element addressing. That sharing is
load-bearing rather than convenient: the extractor *records* locators and the
runtime *resolves* them, so any divergence makes a locator unresolvable. These
were two independent implementations and had silently drifted.

Code under `client/src/shared/` must be realm-agnostic. The extractor only sees
elements from the document it was injected into, but the runtime resolves across
frame boundaries, where an element belongs to another realm and `instanceof
HTMLInputElement` is false. Identify elements structurally instead.

## Extractor

Evaluating `internal/payload/extractor.js` in a browser document installs:

```js
await globalThis.__GEOVISOR_EXTRACT__({
  safeExplore: false,
  maxDepth: 3,
  maxOperations: 20,
  timeoutMs: 1000,
});
```

The result is the JSON form of `observation.Batch`. The payload is frame-local:
all interaction frame paths are empty and `coverageReported` is false. The
browser source owns frame discovery, same-session CDP execution, and combining
frame-local batches.

Safe exploration is opt-in. It performs bounded, direct local state changes
only; currently it may open eligible `<details>` elements, yields so `toggle`
handlers can run, and restores each element's prior `open` state before
returning. `maxDepth: 0` performs no exploration. It never invokes click,
submit, navigation, value-changing, or network APIs. Unknown effects remain
unsafe. `timeoutMs` is a real deadline across that async work. CSS fallback
generation is separately bounded: `@medv/finder` has a short per-call budget
and degrades to a simple unique selector when that budget is exhausted, so
selector search cannot consume the frame extraction deadline.

The extractor never reads current control values. Hidden inputs are skipped,
password fields are represented structurally, and select enums use visible
option labels rather than option values. A positional fallback name is a
parameter-name hint only: it is not written into the interaction name or the
semantic locator, so inserting an unrelated earlier element does not change
unaffected tool IDs. Element-level extraction failures are counted as batch
warnings. Closed shadow roots are invisible to page JavaScript and are
reported by the browser source via CDP.

## WebMCP runtime

`internal/emitter` concatenates a capability guard, the tool definitions, the
runtime bundle, and a call to `__geovisorRuntime.register(...)`. The bundle is
built as an IIFE assigned to a `globalName`, which becomes a module-scoped `var`
in the generated ES module rather than a global.

Because option values are page data that must not leave the page, a `<select>`
tool advertises option labels; the runtime therefore matches an option by label
and sets `selectedIndex` rather than assigning to `value`.

## Checks

Run `npm run typecheck`, `npm test`, and `npm run verify:bundle`. `npm run build`
is the only supported way to update the committed bundles; `verify:bundle` fails
if either one is stale.

`internal/integration/webmcp_roundtrip_test.go` executes the generated module
against the DOM it was extracted from, which is what catches divergence between
the two bundles.

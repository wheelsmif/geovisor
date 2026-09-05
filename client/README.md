# Browser extraction payload

`client/src/extractor.ts` is bundled into
`internal/payload/extractor.js` and embedded by Go. Evaluating the bundle in a
browser document installs:

```js
await globalThis.__GEOVISOR_EXTRACT__({
  safeExplore: false,
  maxDepth: 3,
  maxOperations: 20,
  timeoutMs: 1000,
});
```

The result is the JSON form of `observation.Batch`. The payload is frame-local:
all interaction frame paths are empty and `coverageReported` is false. A future
browser source owns frame discovery, same-session CDP execution, and combining
frame-local batches.

Safe exploration is opt-in. It performs bounded, direct local state changes
only; currently it may open eligible `<details>` elements. It never invokes
click, submit, navigation, value-changing, or network APIs. Unknown effects
remain unsafe. `timeoutMs` only cancels further exploration work.

The extractor never reads current control values. Hidden inputs are skipped,
password fields are represented structurally, and select enums use visible
option labels rather than option values.

Run `npm run typecheck`, `npm test`, and `npm run verify:bundle`. `npm run build`
is the only supported way to update the committed embedded bundle.

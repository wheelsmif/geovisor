# Deterministic TIR compiler

`internal/observation` is the browser-independent input boundary.
`internal/compiler.Compile` accepts all observation batches for one session and
produces canonical TIR. The compiler emits tools, parameters, actions, and enum
values in source order. `tir.Canonical` clones, normalizes, deduplicates, and
validates. It does not sort. Producers own stable ordering (ADR 0002).

Parameter and object-property order follows `SourceOrder` when the observation
sets it. The extractor records document order that way. Without `SourceOrder`,
the compiler orders those names by canonical text. Enum values keep the first
occurrence and drop later duplicates, so a select stays `Small`, `Medium`,
`Large`.

Set-like collections are ordered once, in the compiler. `tir.Canonical` does
not sort them again:

- warnings, in lexicographic code, tool, frame path, and message order
- frame coverage, in lexicographic path-key order
- provenance, in lexicographic kind and reference order

Every enum-valued observation field is rejected at this boundary with a
`compiler.Error` whose `Field` names the input path: source kind, value types,
action kinds, side-effect classes, and evidence kinds. Navigation, submission,
and `unknown` cannot be marked safe to explore. That is a schema and validation
constraint. Safe exploration does not consult `safeForExploration`.

## Identity

Interaction identity consists of interaction kind, frame path, semantic scope,
role, the pre-disambiguation accessible name, and the semantic and path halves
of each locator. CSS is not part of tool identity: path-node CSS fallbacks are
cleared before the identity key is hashed. Display-name ordinals such as
`Query (2)` are not part of identity. Scope identity includes each node's match
ordinal, so two scopes that share a role and a name but select different
elements are distinct and their tools are not merged. This deduplicates
repeated observations without merging equivalent-looking controls in different
frames or semantic scopes, and without changing an unaffected tool's ID when an
unrelated earlier element or same-label control in another scope is inserted.

Frame identity inside a tool is the frame index plus the normalized page URL
(`pageurl.PageURL` of the frame `src`). The frame name is stored for
diagnostics and is not part of that key. `data:` and `blob:` URLs contribute
the scheme only. `about:srcdoc` stays `about:srcdoc`. `about:blank` stays
`about:blank`. Hierarchical URLs keep scheme, host, and path; query strings
and fragments are stripped.

IDs use an ASCII semantic slug, capped so the complete ID stays within 64
characters, plus the first 48 bits of SHA-256 over the length-delimited
identity, written as 12 hexadecimal characters. A family link tool's display
name is `Follow link`, so its id looks like `follow-link-` plus that digest.
`internal/compiler/testdata/compiler.golden.json` shows the same shape on a
form tool: `search-2880204d3af8`. If two complete IDs collide, those identity
keys are sorted. The first keeps the base id; the rest receive `-2`, `-3`,
and so on. That sort assigns suffixes only. It does not reorder the tools
array.

## One tool per capability

A control owned by a form is a parameter of that form's tool and is not also
observed as a standalone tool. Emitting both gives an agent two ways to fill one
field with no basis for choosing between them, and roughly doubles the tool count
on form-heavy pages. A form tool is fill-only: its actions are the fill, select,
and check bindings for its parameters. It never includes a submission click.
Submit, reset, button, and image inputs are actions only, never form parameters
or fill tools. Each submit control remains a standalone action so an agent can
submit without filling, or fill without submitting.

Ownership follows HTML form-associated elements, not a raw `form` attribute on
arbitrary elements, so a control associated with a form by the `form` attribute
is claimed exactly like a nested one. A form with no fillable controls is
skipped. `tir.Validate` rejects a tool that has zero actions.

Same-role, same-action, same-side-effect actions in one frame collapse into one
parameterized family tool when the cohort has two or more members. Links always
compile as `Follow link` whenever any non-junk link remains, including a page
with a single link, so the tool shape does not flip. Family identity is
interaction kind, frame path, role, action kind, and side-effect class. It does
not include accessible name, semantic scope, CSS, or per-element locator
identity.

Family members stay in document order. The display name is `Follow link`,
`Toggle`, or `Click`. The description is one of these exact strings:

- `Follow one of this page's links`
- `Toggle one of this page's controls`
- `Click one of this page's controls`

The tool has one required string parameter, `target`. Its description can still
name the targets. With 1 to 64 names the text is
`Accessible name of the control to use: ` plus the names joined by `, `.
With no names, or more than 64, it is `Accessible name of the control to use`
and the enum is omitted. When the enum is present it is those names in
document order, keeping the first occurrence of a duplicate. The first control
keeps the bare accessible name. Later duplicates are `Name (2)`, `Name (3)`,
and so on. Those ordinals are not part of the tool id. Locator candidates line
up with that enum.

The family's click or check binding sets `inputParameter` to `target` and
`locatorSelection` to `by_target_name`: the parameter value selects one
candidate. Every other action that has locator candidates uses
`locatorSelection` `ranked`: the candidates are ordered fallbacks for one
element, highest confidence first. The apply runtime in
`client/src/apply-runtime.ts` branches on `locatorSelection`, not on the
parameter name. That file is a test harness. GEO-Visor does not ship an
executor.

Submit, reset, and image-submit stay standalone. A unique button whose
role+action+effect cohort in that frame has size 1 stays its own tool. Form
and control tools are unchanged.

Low-value filtering uses the explicit `citationHref` fact and, for an input,
`inputType`. It does not match a regular expression against CSS. Citation
marks (`[1]`), DOI/RFC tokens, and unnamed links are still dropped from the
accessible name. Same-document citation fragments are dropped because
`citationHref` is set. A button whose id is `fn-save` is kept. The extractor
omits low-value actions. The compiler drops any that still arrive. Both record
`dropped_low_value_action`. Unnamed real buttons still receive a fallback
name.

Evidence is deduplicated by provenance kind and reference, retaining the
highest reported score. Confidence is the noisy-or of the unique scores,
rounded to six decimal places. Provenance is emitted in lexicographic
kind+reference order by the compiler. Missing evidence receives a `0.5`
heuristic fallback. Conflicting side-effect reports select the most
conservative class. Safe-for-exploration stays true only when every report is
safe, and the compiler forces it false for navigation, submission, and
`unknown`.

## Warnings

Browser diagnostics that used to exist only on stderr are now also TIR
warnings. Standard error still prints every diagnostic. The codes below are
the ones this tree emits. Messages are the exact strings in the compiler,
extractor, and browser source.

Copied from the observation batch:

- `element_extraction_failed`: `N element(s) could not be extracted`
- `custom_control_omitted`: `N custom widget(s) have no apply path and were omitted`
- `dropped_low_value_action`: `N low-value action(s) have no useful capability and were omitted`
  (extractor). The compiler's own message, when it drops actions that still
  arrive, is `dropped N low-value action(s) that are not useful capabilities`.
- `interaction_limit`: `stopped after 2000 interactions in this frame`. The cap
  is 2000 interactions per frame. Hitting it marks that frame uncovered with
  reason `interaction limit reached before every control was recorded`, which
  is partial coverage.
- `closed_shadow_root`: `closed shadow root on ` plus the host label
- `dom_not_quiet`: `DOM continued changing until the bounded quiet-check deadline`
- `access_interstitial`: `page content resembles a bot challenge or access interstitial`
- `frame_uncovered` from the browser, only for the target-list failure:
  `browser could not list targets for out-of-process frames: ` plus the error.
  Other browser `frame_uncovered` diagnostics are not copied. See
  `docs/browser-sources.md`.

Emitted by the compiler:

- `fallback_name`: `interaction name was derived from semantic context`
- `fallback_confidence`: `interaction confidence defaulted because no evidence was supplied`,
  or `locator confidence defaulted because no evidence was supplied`
- `css_fallback`: `locator relies only on a CSS fallback`
- `missing_locator`: `action has no locator candidates`
- `ambiguous_parameter`: `conflicting definitions for parameter "<name>"; selected the canonical first`
- `ambiguous_side_effect`: `conflicting side-effect classes; selected the most conservative`
- `frame_uncovered`: `frame coverage is incomplete`
- `ambiguous_frame_coverage`: `frame was reported as both accessible and inaccessible; treated as uncovered`
- `ambiguous_frame_metadata`: `conflicting frame metadata; selected the canonical first value`

Coverage is `unavailable` when no batch reports it, `partial` when any known
frame is inaccessible, and `complete` otherwise. Conflicting accessibility
reports are treated conservatively as uncovered. Every fallback, ambiguity,
and uncovered frame produces a structured warning.

`tir.Marshal` and `tir.Write` deep-clone, normalize, deduplicate, and validate
before emitting compact JSON. They do not sort. Both omit a trailing newline
and leave caller-owned documents unchanged. Page URLs in TIR
(`source.requestedUrl`, `source.finalUrl`, frame coverage URLs, and
`FrameReference.src`) pass through `pageurl.PageURL`: hierarchical URLs are
scheme, host, and path; `data:` and `blob:` keep the scheme only;
`about:srcdoc` and `about:blank` stay those tokens. Negative locator `nth`
values are rejected at this boundary and by `tir.Validate`.

# Deterministic TIR compiler

`internal/observation` is the browser-independent input boundary.
`internal/compiler.Compile` accepts all observation batches for one session and
produces canonical TIR. Batch order and unordered input slices do not affect the
result. Parameter and object-property source order is preserved only when an
explicit `SourceOrder` is present; otherwise names provide canonical order.

Every enum-valued observation field is rejected at this boundary with a
`compiler.Error` whose `Field` names the input path: source kind, value types,
action kinds, side-effect classes, and evidence kinds. Navigation, submission,
and unknown cannot be marked safe to explore.

Interaction identity consists of interaction kind, frame path, semantic scope,
role, the pre-disambiguation accessible name, and the semantic and path halves
of each locator, not the CSS fallback. Display-name ordinals such as
`Query (2)` are not part of identity. Scope identity includes each node's match
ordinal, so two scopes that share a role and a name but select different
elements are distinct and their tools are not merged. This deduplicates
repeated observations without merging equivalent-looking controls in different
frames or semantic scopes, and without changing an unaffected tool's ID when an
unrelated earlier element or same-label control in another scope is inserted.

IDs use an ASCII semantic slug (capped so the complete ID stays within 64
characters) plus the first 48 bits of SHA-256 over length-delimited canonical
identity. If those complete IDs collide, identities are sorted and receive
deterministic numeric suffixes.

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
is claimed exactly like a nested one.

Same-role, same-action, same-side-effect actions in one frame collapse into one
parameterized family tool when the cohort has two or more members. Links always
compile as `Follow link` whenever any non-junk link remains, including a page
with a single link, so the tool shape does not flip. Family identity is
interaction kind, frame path, role, action kind, and side-effect class. It does
not include accessible name, semantic scope, or per-element locator identity.
The resulting tool has a stable display name (`Follow link`, `Click`, or
`Toggle`), one required string parameter `target` whose enum is the sorted
unique member display names (existing `Query (2)` ordinals stay, and remain
outside the tool id), one locator candidate per member, and one click or check
binding with `inputParameter: "target"`. If the enum would exceed 64 values it
is omitted and the description states that `target` is the control's accessible
name. Submit, reset, and image-submit stay standalone. A unique button whose
role+action+effect cohort in that frame has size 1 stays its own tool. Form and
control tools are unchanged.

Citation marks (`[1]`), DOI/RFC tokens, same-document citation fragments
(`#cite_note`, `#cite_ref`, `#fn`, `#footnote`), and unnamed links are dropped
as low-value actions. The extractor omits them; the compiler also rejects any
that still arrive and records a `dropped_low_value_action` warning so coverage
stays explicit. Unnamed real buttons still receive a fallback name.

Evidence is deduplicated by provenance kind and reference, retaining the
highest reported score. Confidence is the noisy-or of the sorted unique scores,
rounded to six decimal places. Missing evidence receives a `0.5` heuristic
fallback. Conflicting side-effect reports select the most conservative class;
safe exploration requires every report to be safe and is always false for
navigation, submission, and unknown. Observation-batch warnings (element
extraction failures, closed shadow roots) are copied into the TIR `warnings`
collection.

Coverage is `unavailable` when no batch reports it, `partial` when any known
frame is inaccessible, and `complete` otherwise. Conflicting accessibility
reports are treated conservatively as uncovered. Every fallback, ambiguity,
and uncovered frame produces a structured warning.

`tir.Marshal` and `tir.Write` deep-clone, normalize, canonically order, and
validate before emitting compact JSON. Both omit a trailing newline and never
mutate caller-owned documents. Page URLs in TIR (`source.requestedUrl`,
`source.finalUrl`, frame coverage URLs, and `FrameReference.src`) are
origin+path only: query strings and fragments are stripped at the compiler
boundary. Negative locator `nth` values are rejected at this boundary and by
`tir.Validate`.

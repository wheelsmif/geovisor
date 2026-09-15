import { finder } from "@medv/finder";

import type {
  Action,
  Batch,
  Evidence,
  ExtractionOptions,
  Interaction,
  Locator,
  Parameter,
  PathNode,
  SemanticLocator,
  SemanticNode,
  SideEffect,
  ValueType,
  Warning,
} from "./types";
import { cleanText, DESCRIPTION_LIMIT, humanize } from "./shared/text";
import {
  beginReadAccounting,
  explicitRole,
  inputType,
  parentAcrossShadow,
  read,
  readExpected,
  referencedText,
} from "./shared/dom";
import {
  DEFAULT_MAX_DEPTH,
  DEFAULT_MAX_OPERATIONS,
  DEFAULT_TIMEOUT_MS,
  MAX_DEPTH,
  MAX_OPERATIONS,
  MAX_TIMEOUT_MS,
} from "./shared/limits";
import { GENERIC_ROLE, isContentEditable, semanticRole } from "./shared/role";
import { semanticMatches } from "./shared/locate";
import { isCitationFragment, isLowValueName } from "./shared/low-value";
import { accessibleName, type NameSource } from "./shared/name";
import { optionLabels } from "./shared/option";

/** Per-call @medv/finder budget. Exhaustion falls back to simpleSelector (GV-033). */
const FINDER_TIMEOUT_MS = 50;

/** Custom widgets the runtime can operate as a click, not a native apply. */
const CLICKABLE_CUSTOM_ROLES = new Set(["checkbox", "radio", "switch"]);
/** Custom widgets with no apply path; omit rather than register a throwing tool. */
const OMITTED_CUSTOM_ROLES = new Set([
  "combobox",
  "searchbox",
  "slider",
  "spinbutton",
  "textbox",
]);
const ACTION_ROLES = new Set([
  "button",
  "link",
  "menuitem",
  "menuitemcheckbox",
  "menuitemradio",
  "option",
  "tab",
  "treeitem",
]);
const LANDMARK_ROLES = new Set([
  "complementary",
  "dialog",
  "form",
  "group",
  "main",
  "navigation",
  "region",
  "search",
]);

interface ElementRecord {
  element: Element;
  shadowPath: PathNode[];
  sourceOrder: number;
  /** Resolved by `traverse`, which is the only place ancestors are known. */
  hidden: boolean;
  disabled: boolean;
}

interface LabelResult {
  text: string;
  reference: NameSource | "deterministic-fallback";
  score: number;
}

interface ExplorationResult {
  details: Set<Element>;
}

interface NormalizedOptions {
  safeExplore: boolean;
  maxDepth: number;
  maxOperations: number;
  timeoutMs: number;
}

function clampInteger(value: number | undefined, fallback: number, maximum: number): number {
  if (value === undefined || !Number.isFinite(value)) return fallback;
  return Math.max(0, Math.min(maximum, Math.floor(value)));
}

function normalizeOptions(options: ExtractionOptions | undefined): NormalizedOptions {
  return {
    safeExplore: options?.safeExplore === true,
    maxDepth: clampInteger(options?.maxDepth, DEFAULT_MAX_DEPTH, MAX_DEPTH),
    maxOperations: clampInteger(
      options?.maxOperations,
      DEFAULT_MAX_OPERATIONS,
      MAX_OPERATIONS,
    ),
    timeoutMs: clampInteger(options?.timeoutMs, DEFAULT_TIMEOUT_MS, MAX_TIMEOUT_MS),
  };
}

/**
 * Reports whether an element hides everything inside it, not just itself.
 *
 * `display: none` is the reason GV-002 needed an ancestor walk at all: it is not
 * reflected in a descendant's computed style, because only the *used* value is
 * affected, so a descendant of a `display: none` wrapper still reports its own
 * `display`. The `hidden` attribute resolves to `display: none` through the UA
 * stylesheet, and `aria-hidden="true"` hides the whole subtree from the
 * accessibility tree, with `aria-hidden="false"` on a descendant not undoing it.
 */
function hidesSubtree(element: Element): boolean {
  if (
    element.hasAttribute("inert") ||
    element.hasAttribute("hidden") ||
    element.getAttribute("aria-hidden") === "true"
  ) {
    return true;
  }
  return read(false, () => getComputedStyle(element).display === "none");
}

function disablesSubtree(element: Element): boolean {
  return element.localName === "fieldset" && element.hasAttribute("disabled");
}

function isDisabledHere(element: Element): boolean {
  return element.hasAttribute("disabled") || disablesSubtree(element);
}

function isReadonlyControl(element: Element): boolean {
  return element.hasAttribute("readonly") || element.getAttribute("aria-readonly") === "true";
}

/**
 * Reports whether an element is hidden by its own attributes or style.
 *
 * Ancestor-inherited hiding is *not* handled here; `traverse` carries that down
 * the tree. `visibility` is deliberately checked locally: unlike `display`, it
 * is an inherited CSS property, so the computed value already accounts for
 * ancestors and still honors a descendant's `visibility: visible` override.
 */
function isHiddenLocally(element: Element): boolean {
  if (element.localName === "input" && inputType(element) === "hidden") return true;
  if (hidesSubtree(element)) return true;
  return read(false, () => getComputedStyle(element).visibility === "hidden");
}

/**
 * Resolves the display name for an element.
 *
 * The recomputable sources live in `shared/name.ts` so the generated runtime
 * derives the same name when it looks the element up again. Only the positional
 * fallback is local: it depends on a document-wide traversal counter the runtime
 * cannot know, so it is a parameter-name hint and never a match key or tool
 * identity (GV-018).
 */
function labelFor(element: Element, role: string, fallbackIndex: number): LabelResult {
  const derived = accessibleName(element, role);
  if (derived) return { text: derived.text, reference: derived.source, score: derived.score };
  return {
    text: `${humanize(role || element.localName)} ${fallbackIndex + 1}`,
    reference: "deterministic-fallback",
    score: 0.35,
  };
}

/** Display / identity name. Positional fallbacks stay empty so they cannot churn IDs. */
function recordedName(label: LabelResult): string {
  return label.reference === "deterministic-fallback" ? "" : label.text;
}

/**
 * The name the shared matcher should require. `undefined` means "any name",
 * which is what a positional fallback must use: an empty string would be
 * dropped by TIR `omitempty` and then match the wrong element.
 */
function locatorMatchName(label: LabelResult): string | undefined {
  return label.reference === "deterministic-fallback" ? undefined : label.text;
}

function cssEscape(value: string): string {
  if (typeof CSS !== "undefined" && typeof CSS.escape === "function") return CSS.escape(value);
  return value.replace(/[^a-zA-Z0-9_-]/gu, (character) => `\\${character}`);
}

function selectorParent(element: Element): ParentNode | null {
  if (element.parentElement) return element.parentElement;
  const parent = element.parentNode;
  if (
    parent &&
    (parent.nodeType === 9 || parent.nodeType === 11) &&
    "children" in parent
  ) {
    return parent;
  }
  return null;
}

function simpleSelector(element: Element): string {
  const id = cleanText(element.id);
  if (id) return `#${cssEscape(id)}`;
  const parent = selectorParent(element);
  const tag = element.localName;
  if (!parent) return tag;
  const siblings = Array.from(parent.children).filter((sibling) => sibling.localName === tag);
  const index = siblings.indexOf(element);
  return index <= 0 && siblings.length === 1 ? tag : `${tag}:nth-of-type(${index + 1})`;
}

function cssFallback(element: Element): string {
  const fallback = simpleSelector(element);
  if (fallback.startsWith("#")) return fallback;
  const root = element.getRootNode();
  if (root.nodeType !== 9 && root.nodeType !== 11) {
    return fallback;
  }
  const scoped = root as Document | ShadowRoot;
  const found = readExpected(fallback, () =>
    finder(element, {
      root: root as unknown as Element,
      attr: (name) => name === "role" || name === "type",
      className: () => false,
      idName: () => false,
      tagName: () => true,
      timeoutMs: FINDER_TIMEOUT_MS,
      seedMinLength: 1,
      optimizedMinLength: 2,
      maxNumberOfPathChecks: 5_000,
    }),
  );
  if (found !== fallback && selectorUniquelyMatches(scoped, found, element)) {
    return found;
  }
  return fallback;
}

function selectorUniquelyMatches(root: Document | ShadowRoot, selector: string, element: Element): boolean {
  return (
    read(false, () => {
      const matches = root.querySelectorAll(selector);
      return matches.length === 1 && matches[0] === element;
    }) === true
  );
}

function pathNode(element: Element, sourceOrder: number): PathNode {
  const role = semanticRole(element);
  const label = labelFor(element, role || "host", sourceOrder);
  const node: PathNode = { css: cssFallback(element) };
  const name = recordedName(label);
  if (role) {
    node.semantic = name ? { role, name } : { role };
  } else if (name) {
    node.semantic = { role: GENERIC_ROLE, name };
  }
  return node;
}

/**
 * Walks the document in source order, recording each element with whether it is
 * hidden.
 *
 * This is a depth-first child walk rather than a flat `querySelectorAll("*")`
 * scan so that hiding can be inherited from ancestors (GV-002) and across
 * shadow boundaries: a hidden host hides its shadow tree. `sourceOrder` is a
 * parameter-ordering hint only; tool identity does not read it (GV-018).
 */
function traverse(root: Document | ShadowRoot): ElementRecord[] {
  const records: ElementRecord[] = [];
  let sourceOrder = 0;

  const visit = (
    parent: Document | ShadowRoot | Element,
    shadowPath: PathNode[],
    inheritedHidden: boolean,
    inheritedDisabled: boolean,
  ): void => {
    for (const element of Array.from(parent.children)) {
      const hidden = inheritedHidden || read(true, () => isHiddenLocally(element));
      const disabled = inheritedDisabled || read(false, () => isDisabledHere(element));
      records.push({ element, shadowPath, sourceOrder: sourceOrder++, hidden, disabled });
      // Only subtree-hiding conditions propagate. `visibility: hidden` does not,
      // because a descendant may set `visibility: visible`.
      const subtreeHidden = inheritedHidden || read(true, () => hidesSubtree(element));
      const subtreeDisabled = inheritedDisabled || read(false, () => disablesSubtree(element));

      const shadowRoot = read<ShadowRoot | null>(null, () => element.shadowRoot);
      if (shadowRoot?.mode === "open") {
        const hostPath = read<PathNode>(
          { css: read(element.localName, () => simpleSelector(element)) },
          () => pathNode(element, sourceOrder),
        );
        visit(shadowRoot, [...shadowPath, hostPath], subtreeHidden, subtreeDisabled);
      }
      visit(element, shadowPath, subtreeHidden, subtreeDisabled);
    }
  };

  visit(root, [], false, false);
  return records;
}

function documentOrShadowRoot(element: Element): Document | ShadowRoot | null {
  return read<Document | ShadowRoot | null>(null, () => {
    const node = element.getRootNode();
    return node instanceof Document || node instanceof ShadowRoot ? node : null;
  });
}

function semanticScope(element: Element): SemanticNode[] {
  const landmarks: { element: Element; role: string; name: LabelResult }[] = [];
  let current = parentAcrossShadow(element);
  while (current && landmarks.length < 4) {
    const role = semanticRole(current);
    if (LANDMARK_ROLES.has(role)) {
      const name = labelFor(current, role, 0);
      if (name.reference !== "deterministic-fallback") {
        landmarks.push({ element: current, role, name });
      }
    }
    current = parentAcrossShadow(current);
  }
  landmarks.reverse();

  const nodes: SemanticNode[] = [];
  let scopeRoot: Document | ShadowRoot | Element | null = documentOrShadowRoot(element);
  for (const landmark of landmarks) {
    const node: SemanticNode = { role: landmark.role, name: landmark.name.text };
    if (scopeRoot) {
      const matches = read<Element[]>([], () =>
        semanticMatches(scopeRoot as Document | ShadowRoot | Element, {
          role: landmark.role,
          name: landmark.name.text,
        }),
      );
      const nth = matches.indexOf(landmark.element);
      if (nth < 0) continue;
      if (matches.length > 1) node.nth = nth;
    }
    nodes.push(node);
    scopeRoot = landmark.element;
  }
  return nodes;
}

function locatorFor(record: ElementRecord, role: string, name?: string): Locator {
  const semantic = verifiedSemantic(record.element, role, name);
  const dom: Evidence = {
    kind: "dom",
    reference: `css:${record.element.localName}`,
    score: 0.65,
  };
  return {
    framePath: [],
    shadowPath: record.shadowPath.map((node) => ({
      ...(node.semantic ? { semantic: { ...node.semantic } } : {}),
      ...(node.css ? { css: node.css } : {}),
    })),
    ...(semantic ? { semantic } : {}),
    css: cssFallback(record.element),
    evidence: semantic
      ? [{ kind: "accessibility", reference: `semantic:${role}`, score: 0.88 }, dom]
      : [dom],
  };
}

/**
 * Builds a semantic locator, but only one that resolves back to `element`.
 *
 * The check runs the shared matcher the runtime will run. Recording an
 * unverified locator is how GV-003, GV-004, and GV-049 all failed: the
 * semantic strategy silently missed, the runtime fell through to the CSS
 * fallback, and nothing reported that the precise strategy was dead. An
 * unverifiable locator now records no semantic half at all, which is honest and
 * leaves the CSS fallback as the only claim.
 *
 * When more than one element matches, the element's index within the match set
 * is recorded. That is the replacement for mutating the name into something
 * unique -- a mutated name matches nothing (GV-004).
 */
function verifiedSemantic(
  element: Element,
  role: string,
  name?: string,
): SemanticLocator | undefined {
  const root = documentOrShadowRoot(element);
  if (!root) return undefined;

  const semantic: SemanticLocator = { scope: semanticScope(element), role };
  if (name !== undefined) semantic.name = name;
  const matches = read<Element[]>([], () => semanticMatches(root, semantic));
  const nth = matches.indexOf(element);
  if (nth < 0) return undefined;
  return matches.length > 1 ? { ...semantic, nth } : semantic;
}

function evidenceFor(label: LabelResult, element: Element): Evidence[] {
  const kind = label.reference.startsWith("aria") || label.reference === "label"
    ? "accessibility"
    : label.reference === "deterministic-fallback" || label.reference === "semantic-context"
      ? "heuristic"
      : "dom";
  return [
    { kind, reference: `name:${label.reference}`, score: label.score },
    { kind: "dom", reference: `element:${element.localName}`, score: 0.6 },
  ];
}

function controlValueType(element: Element): ValueType {
  if (element instanceof HTMLInputElement) {
    switch (inputType(element)) {
      case "checkbox":
      case "radio":
        return "boolean";
      case "number":
      case "range":
        return element.getAttribute("step") === "1" ? "integer" : "number";
      default:
        return "string";
    }
  }
  const role = explicitRole(element);
  if (role === "checkbox" || role === "radio" || role === "switch") {
    return "boolean";
  }
  if (role === "slider" || role === "spinbutton") {
    return "number";
  }
  return "string";
}

function controlEnum(element: Element): string[] {
  if (!(element instanceof HTMLSelectElement)) return [];
  return optionLabels(element);
}

function parameterName(label: string): string {
  const words = cleanText(label)
    .normalize("NFKD")
    .replace(/[^\p{Letter}\p{Number}]+/gu, " ")
    .trim()
    .split(" ")
    .filter(Boolean);
  if (words.length === 0) return "value";
  const [first, ...rest] = words;
  let result =
    first!.toLocaleLowerCase("en-US") +
    rest.map((word) => word[0]!.toLocaleUpperCase("en-US") + word.slice(1)).join("");
  if (/^\p{Number}/u.test(result)) {
    result = `value${result}`;
  }
  return Array.from(result).slice(0, 80).join("");
}

function parameterFor(
  element: Element,
  label: LabelResult,
  sourceOrder: number,
  name = parameterName(label.text),
): Parameter {
  const description =
    element instanceof HTMLInputElement && inputType(element) === "password"
      ? "Sensitive password field; its current value is never observed."
      : undefined;
  return {
    name,
    ...(description ? { description } : {}),
    type: controlValueType(element),
    required: element.hasAttribute("required") || element.getAttribute("aria-required") === "true",
    enum: controlEnum(element),
    properties: [],
    sourceOrder,
  };
}

function controlAction(element: Element, parameter: string, locatorIndex = 0): Action {
  let kind: Action["kind"] = "fill";
  if (element instanceof HTMLSelectElement || explicitRole(element) === "combobox") kind = "select";
  if (
    (element instanceof HTMLInputElement &&
      (inputType(element) === "checkbox" || inputType(element) === "radio")) ||
    ["checkbox", "radio", "switch"].includes(explicitRole(element))
  ) {
    kind = "check";
  }
  return {
    kind,
    inputParameter: parameter,
    locatorIndexes: [locatorIndex],
    sideEffect: {
      class: "unknown",
      safeForExploration: false,
      rationale: "Changing a control may invoke page handlers; extraction never changes its value.",
    },
  };
}

function isButtonLikeInput(element: Element): boolean {
  return (
    element.localName === "input" &&
    ["button", "image", "reset", "submit"].includes(inputType(element))
  );
}

function isNativeControl(element: Element): boolean {
  if (isContentEditable(element)) return true;
  if (!element.matches("input, select, textarea")) return false;
  const tag = element.localName;
  if (tag === "select" || tag === "textarea") return true;
  const type = inputType(element);
  return type !== "hidden" && type !== "file" && !isButtonLikeInput(element);
}

function isFillableControl(element: Element): boolean {
  return isNativeControl(element) && !isReadonlyControl(element);
}

function isCustomClickable(element: Element): boolean {
  if (isNativeControl(element) || isButtonLikeInput(element)) return false;
  return CLICKABLE_CUSTOM_ROLES.has(explicitRole(element));
}

function isOmittedCustomWidget(element: Element): boolean {
  if (isNativeControl(element) || isButtonLikeInput(element)) return false;
  return OMITTED_CUSTOM_ROLES.has(explicitRole(element));
}

function isAction(element: Element): boolean {
  if (element.matches("button, a[href], summary")) return true;
  if (isButtonLikeInput(element)) return true;
  if (isCustomClickable(element)) return true;
  return ACTION_ROLES.has(explicitRole(element));
}

function isCitationHref(element: Element): boolean {
  const href = element.getAttribute("href");
  if (!href) return false;
  const raw = href.trim();
  let fragment = "";
  let sameDocument = false;
  if (raw.startsWith("#")) {
    fragment = raw;
    sameDocument = true;
  } else {
    try {
      const resolved = new URL(raw, document.baseURI);
      const page = new URL(document.baseURI);
      fragment = resolved.hash;
      sameDocument = resolved.origin === page.origin && resolved.pathname === page.pathname;
    } catch {
      return false;
    }
  }
  return sameDocument && isCitationFragment(fragment);
}

/** Citation marks, DOI/RFC tokens, same-document cite fragments, and unnamed links. */
function isLowValueAction(element: Element): boolean {
  if (isCitationHref(element)) return true;
  const role = semanticRole(element) || GENERIC_ROLE;
  const name = accessibleName(element, role)?.text ?? "";
  return isLowValueName(name, role);
}

function isFormAssociated(element: Element): boolean {
  const tag = element.localName;
  return (
    tag === "button" ||
    tag === "fieldset" ||
    tag === "input" ||
    tag === "object" ||
    tag === "output" ||
    tag === "select" ||
    tag === "textarea" ||
    tag === "img"
  );
}

function actionEffect(element: Element): SideEffect {
  if (element.matches("a[href]") || explicitRole(element) === "link") {
    return {
      class: "navigation",
      safeForExploration: false,
      rationale: "Following a link may navigate and is never explored.",
    };
  }
  if (
    (element instanceof HTMLButtonElement && element.type === "submit" && element.form !== null) ||
    (element instanceof HTMLInputElement &&
      (inputType(element) === "submit" || inputType(element) === "image") &&
      element.form !== null)
  ) {
    return {
      class: "submission",
      safeForExploration: false,
      rationale: "Form submission is always unsafe for exploration.",
    };
  }
  if (
    element.localName === "summary" ||
    explicitRole(element) === "tab" ||
    element.hasAttribute("aria-expanded") ||
    element.hasAttribute("aria-controls") ||
    element.getAttribute("command") === "show-modal"
  ) {
    return {
      class: "local_state",
      safeForExploration: false,
      rationale: "The trigger appears local, but extraction never clicks elements.",
    };
  }
  return {
    class: "unknown",
    safeForExploration: false,
    rationale: "Click effects cannot be proven safe statically.",
  };
}

function descriptionFor(element: Element): string {
  return cleanText(
    referencedText(element, "aria-describedby") || element.getAttribute("title"),
    DESCRIPTION_LIMIT,
  );
}

function interactionIdentity(record: ElementRecord): {
  role: string;
  label: LabelResult;
  description: string;
  scope: SemanticNode[];
  locators: Locator[];
  evidence: Evidence[];
} {
  const role = semanticRole(record.element) || GENERIC_ROLE;
  const label = labelFor(record.element, role, record.sourceOrder);
  return {
    role,
    label,
    description: descriptionFor(record.element),
    scope: semanticScope(record.element),
    locators: [locatorFor(record, role, locatorMatchName(label))],
    evidence: evidenceFor(label, record.element),
  };
}

function controlInteraction(record: ElementRecord): Interaction {
  const identity = interactionIdentity(record);
  const parameter = parameterFor(record.element, identity.label, record.sourceOrder);
  return {
    kind: "control",
    framePath: [],
    scope: identity.scope,
    role: identity.role,
    name: recordedName(identity.label),
    ...(identity.description ? { description: identity.description } : {}),
    parameters: [parameter],
    locators: identity.locators,
    actions: [controlAction(record.element, parameter.name)],
    evidence: identity.evidence,
  };
}

function actionInteraction(record: ElementRecord): Interaction {
  const identity = interactionIdentity(record);
  return {
    kind: "action",
    framePath: [],
    scope: identity.scope,
    role: identity.role,
    name: recordedName(identity.label),
    ...(identity.description ? { description: identity.description } : {}),
    parameters: [],
    locators: identity.locators,
    actions: [
      {
        kind: "click",
        locatorIndexes: [0],
        sideEffect: actionEffect(record.element),
      },
    ],
    evidence: identity.evidence,
  };
}

function associatedForm(element: Element): HTMLFormElement | null {
  if (isFormAssociated(element) && "form" in element) {
    const form = (element as HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement | HTMLButtonElement)
      .form;
    return form ?? null;
  }
  return element.closest("form");
}

/** The visible, enabled, form-associated fillable controls a form owns, in source order. */
function formMembers(form: HTMLFormElement, records: ElementRecord[]): ElementRecord[] {
  return records.filter(
    (record) =>
      !record.hidden &&
      !record.disabled &&
      isFillableControl(record.element) &&
      associatedForm(record.element) === form,
  );
}

function formInteraction(formRecord: ElementRecord, members: ElementRecord[]): Interaction {
  const form = formRecord.element as HTMLFormElement;
  const role = explicitRole(form) || "form";
  const label = labelFor(form, role, formRecord.sourceOrder);
  const names = new Map<string, number>();
  const parameters: Parameter[] = [];
  const locators: Locator[] = [];
  const actions: Action[] = [];

  for (const record of members) {
    const controlRole = semanticRole(record.element) || GENERIC_ROLE;
    const controlLabel = labelFor(record.element, controlRole, record.sourceOrder);
    const base = parameterName(controlLabel.text);
    const occurrence = (names.get(base) ?? 0) + 1;
    names.set(base, occurrence);
    const name = occurrence === 1 ? base : `${base}_${occurrence}`;
    parameters.push(parameterFor(record.element, controlLabel, record.sourceOrder, name));
    locators.push(locatorFor(record, controlRole, locatorMatchName(controlLabel)));
    actions.push(controlAction(record.element, name, locators.length - 1));
  }

  const formLocator = locatorFor(formRecord, role, locatorMatchName(label));
  locators.unshift(formLocator);
  for (const action of actions) {
    action.locatorIndexes = action.locatorIndexes.map((index) => index + 1);
  }

  const description = descriptionFor(form);
  return {
    kind: "form",
    framePath: [],
    scope: semanticScope(form),
    role,
    name: recordedName(label),
    ...(description ? { description } : {}),
    parameters,
    locators,
    actions,
    evidence: evidenceFor(label, form),
  };
}

function detailsDepth(element: Element): number {
  let depth = 0;
  let current = parentAcrossShadow(element);
  while (current) {
    if (current.localName === "details") depth++;
    current = parentAcrossShadow(current);
  }
  return depth;
}

/** A macrotask, so `toggle` handlers and layout run before the next open. */
function yieldToEventLoop(): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, 0);
  });
}

interface OpenedDetails {
  element: HTMLDetailsElement;
  wasOpen: boolean;
}

/**
 * Opens eligible `<details>` so a later traverse can see revealed controls.
 *
 * The caller must invoke `restore` after that traverse (and on any failure)
 * so attach mode does not leave a live tab mutated (GV-008). Opening is
 * bounded by depth, operations, and a real deadline across the yields
 * (GV-009, GV-010). `--depth 0` is no exploration: a top-level `<details>`
 * has depth 0, so the guard is `depth < maxDepth`.
 */
async function exploreSafely(
  records: ElementRecord[],
  options: NormalizedOptions,
): Promise<{ exploration: ExplorationResult; restore: () => void }> {
  const exploration: ExplorationResult = { details: new Set() };
  const snapshots: OpenedDetails[] = [];
  for (const record of records) {
    if (record.element.localName !== "details") continue;
    snapshots.push({
      element: record.element as HTMLDetailsElement,
      wasOpen: (record.element as HTMLDetailsElement).open,
    });
  }
  const restore = (): void => {
    for (const item of snapshots) {
      item.element.open = item.wasOpen;
    }
  };
  if (!options.safeExplore || options.maxOperations === 0 || options.maxDepth === 0) {
    return { exploration, restore };
  }

  try {
    const deadline = performance.now() + options.timeoutMs;
    let operations = 0;
    for (const record of records) {
      if (operations >= options.maxOperations || performance.now() >= deadline) break;
      const element = record.element;
      if (
        !(element instanceof HTMLDetailsElement) ||
        element.hasAttribute("disabled") ||
        element.open ||
        detailsDepth(element) >= options.maxDepth
      ) {
        continue;
      }
      operations++;
      exploration.details.add(element);
      element.open = true;
      await yieldToEventLoop();
      if (performance.now() >= deadline) break;
    }
    return { exploration, restore };
  } catch (error) {
    restore();
    throw error;
  }
}

function explorationEvidence(
  interaction: Interaction,
  element: Element,
  exploration: ExplorationResult,
): void {
  if (element.localName === "summary") {
    const details = element.parentElement;
    if (details && exploration.details.has(details)) {
      interaction.evidence.push({
        kind: "heuristic",
        reference: "exploration:details-opened-without-click",
        score: 0.7,
      });
    }
  }
}

function disambiguateInteractions(interactions: Interaction[]): void {
  const occurrences = new Map<string, number>();
  for (const interaction of interactions) {
    const scope = interaction.scope.map((node) => `${node.role}\u0000${node.name ?? ""}`).join("\u0001");
    const key = `${interaction.kind}\u0002${scope}\u0002${interaction.role}\u0002${interaction.name.toLocaleLowerCase("en-US")}`;
    const occurrence = (occurrences.get(key) ?? 0) + 1;
    occurrences.set(key, occurrence);
    if (occurrence > 1 && interaction.name) {
      // Only the display name is disambiguated. The locator keeps the name the
      // element actually carries, because that is the only name the runtime can
      // recompute; ambiguity is resolved by the ordinal in the semantic locator
      // instead (GV-004). Positional fallbacks leave name empty so they are
      // not rewritten into a document-wide index (GV-018).
      interaction.name = `${interaction.name} (${occurrence})`;
      interaction.evidence.push({
        kind: "heuristic",
        reference: "identity:duplicate-name-disambiguated",
        score: 0.5,
      });
    }
  }
}

export async function extract(options?: ExtractionOptions): Promise<Batch> {
  const normalized = normalizeOptions(options);
  const finishReads = beginReadAccounting();
  let records = traverse(document);
  const { exploration, restore } = await exploreSafely(records, normalized);
  try {
    if (exploration.details.size > 0) records = traverse(document);

    // Forms claim their fillable controls first (GV-007). A control owned by a
    // form is a parameter of that form's tool and is not also emitted standalone.
    // The form tool is fill-only: submission stays a standalone action (P1).
    // Submit/reset/button/image inputs are actions, never parameters (P2).
    const formMemberIndex = new Map<HTMLFormElement, ElementRecord[]>();
    const claimed = new Set<Element>();
    for (const record of records) {
      if (record.hidden || !(record.element instanceof HTMLFormElement)) continue;
      const members = read<ElementRecord[]>([], () => formMembers(record.element as HTMLFormElement, records));
      formMemberIndex.set(record.element, members);
      for (const member of members) {
        claimed.add(member.element);
      }
    }

    const interactions: Interaction[] = [];
    let elementFailures = 0;
    let omittedCustom = 0;
    for (const record of records) {
      if (record.hidden || record.disabled) continue;
      const element = record.element;
      try {
        // The branches are exclusive: one record must not yield two interactions.
        if (element instanceof HTMLFormElement) {
          interactions.push(formInteraction(record, formMemberIndex.get(element) ?? []));
        } else if (claimed.has(element)) {
          // Already a parameter of its form's tool.
        } else if (isOmittedCustomWidget(element)) {
          omittedCustom++;
        } else if (isFillableControl(element)) {
          const interaction = controlInteraction(record);
          explorationEvidence(interaction, element, exploration);
          interactions.push(interaction);
        } else if (isAction(element)) {
          if (isLowValueAction(element)) {
            continue;
          }
          const interaction = actionInteraction(record);
          explorationEvidence(interaction, element, exploration);
          interactions.push(interaction);
        }
      } catch {
        // Isolate the failure so the remaining observations are still returned,
        // and count it so the gap is explicit (GV-030).
        elementFailures++;
      }
    }
    disambiguateInteractions(interactions);

    elementFailures += finishReads();
    const warnings: Warning[] = [];
    if (elementFailures > 0) {
      warnings.push({
        code: "element_extraction_failed",
        message: `${elementFailures} element(s) could not be extracted`,
      });
    }
    if (omittedCustom > 0) {
      warnings.push({
        code: "custom_control_omitted",
        message: `${omittedCustom} custom widget(s) have no apply path and were omitted`,
      });
    }

    return {
      coverageReported: false,
      frames: [],
      interactions,
      warnings,
    };
  } finally {
    finishReads();
    restore();
  }
}

globalThis.__GEOVISOR_EXTRACT__ = extract;

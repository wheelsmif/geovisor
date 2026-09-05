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
  SemanticNode,
  SideEffect,
  ValueType,
} from "./types";

const TEXT_LIMIT = 256;
const DESCRIPTION_LIMIT = 512;
const DEFAULT_MAX_DEPTH = 3;
const DEFAULT_MAX_OPERATIONS = 20;
const DEFAULT_TIMEOUT_MS = 1_000;
const MAX_DEPTH = 16;
const MAX_OPERATIONS = 500;
const MAX_TIMEOUT_MS = 10_000;

const CUSTOM_CONTROL_ROLES = new Set([
  "checkbox",
  "combobox",
  "radio",
  "searchbox",
  "slider",
  "spinbutton",
  "switch",
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
}

interface LabelResult {
  text: string;
  reference: string;
  score: number;
}

interface ExplorationResult {
  details: Set<Element>;
  focused: Set<Element>;
}

interface NormalizedOptions {
  safeExplore: boolean;
  maxDepth: number;
  maxOperations: number;
  timeoutMs: number;
}

function cleanText(value: string | null | undefined, limit = TEXT_LIMIT): string {
  if (!value) return "";
  return value.replace(/\s+/gu, " ").trim().slice(0, limit);
}

function read<T>(fallback: T, operation: () => T): T {
  try {
    return operation();
  } catch {
    return fallback;
  }
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

function explicitRole(element: Element): string {
  return cleanText(element.getAttribute("role")).toLowerCase().split(" ")[0] ?? "";
}

function inputType(element: HTMLInputElement): string {
  return cleanText(element.getAttribute("type") ?? "text").toLowerCase() || "text";
}

function semanticRole(element: Element): string {
  const explicit = explicitRole(element);
  if (explicit) return explicit;

  const tag = element.localName;
  if (tag === "form") return "form";
  if (tag === "nav") return "navigation";
  if (tag === "button" || tag === "summary") return "button";
  if (tag === "a" && element.hasAttribute("href")) return "link";
  if (tag === "textarea" || element.hasAttribute("contenteditable")) return "textbox";
  if (tag === "select") {
    return (element as HTMLSelectElement).multiple ? "listbox" : "combobox";
  }
  if (tag === "input") {
    switch (inputType(element as HTMLInputElement)) {
      case "button":
      case "image":
      case "reset":
      case "submit":
        return "button";
      case "checkbox":
        return "checkbox";
      case "radio":
        return "radio";
      case "range":
        return "slider";
      case "number":
        return "spinbutton";
      case "search":
        return "searchbox";
      default:
        return "textbox";
    }
  }
  if (tag === "dialog") return "dialog";
  if (tag === "details" || tag === "fieldset") return "group";
  if (/^h[1-6]$/u.test(tag)) return "heading";
  return "";
}

function isHidden(element: Element): boolean {
  if (element.localName === "input" && inputType(element as HTMLInputElement) === "hidden") {
    return true;
  }
  if (element.hasAttribute("hidden") || element.getAttribute("aria-hidden") === "true") {
    return true;
  }
  return read(false, () => {
    const style = getComputedStyle(element);
    return style.display === "none" || style.visibility === "hidden";
  });
}

function referencedText(element: Element, attribute: string): string {
  const ids = cleanText(element.getAttribute(attribute), DESCRIPTION_LIMIT).split(" ").filter(Boolean);
  const root = element.getRootNode();
  if (!(root instanceof Document || root instanceof ShadowRoot)) return "";
  return cleanText(
    ids
      .map((id) => read("", () => cleanText(root.getElementById(id)?.textContent)))
      .filter(Boolean)
      .join(" "),
  );
}

function labelsText(element: Element): string {
  if (!(element instanceof HTMLInputElement ||
        element instanceof HTMLSelectElement ||
        element instanceof HTMLTextAreaElement)) {
    return "";
  }
  return cleanText(
    Array.from(element.labels ?? [])
      .map((label) => cleanText(label.textContent))
      .filter(Boolean)
      .join(" "),
  );
}

function ownActionText(element: Element): string {
  if (
    element.matches(
      "button, a[href], summary, [role='button'], [role='link'], [role='tab'], [role^='menuitem']",
    )
  ) {
    return cleanText(element.textContent);
  }
  if (element instanceof HTMLInputElement) {
    const type = inputType(element);
    if (type === "image") return cleanText(element.getAttribute("alt"));
  }
  return "";
}

function adjacentText(element: Element): string {
  const before = element.previousElementSibling;
  if (before) {
    const text = cleanText(before.textContent);
    if (text && text.length <= 120) return text;
  }
  const parent = element.parentElement;
  if (!parent) return "";
  const direct = Array.from(parent.childNodes)
    .filter((node) => node.nodeType === Node.TEXT_NODE)
    .map((node) => cleanText(node.textContent))
    .filter(Boolean)
    .join(" ");
  return direct.length <= 120 ? cleanText(direct) : "";
}

function parentAcrossShadow(element: Element): Element | null {
  if (element.parentElement) return element.parentElement;
  const root = element.getRootNode();
  return root instanceof ShadowRoot ? root.host : null;
}

function contextName(element: Element): string {
  let current = parentAcrossShadow(element);
  while (current) {
    const labelled = referencedText(current, "aria-labelledby");
    const aria = cleanText(current.getAttribute("aria-label"));
    const legend =
      current instanceof HTMLFieldSetElement
        ? cleanText(current.querySelector(":scope > legend")?.textContent)
        : "";
    const heading = cleanText(current.querySelector(":scope > h1, :scope > h2, :scope > h3")?.textContent);
    const name = labelled || aria || legend || heading;
    if (name) return name;
    current = parentAcrossShadow(current);
  }
  return "";
}

function labelFor(element: Element, role: string, fallbackIndex: number): LabelResult {
  const label = labelsText(element);
  if (label) return { text: label, reference: "label", score: 0.98 };

  const labelled = referencedText(element, "aria-labelledby");
  if (labelled) return { text: labelled, reference: "aria-labelledby", score: 0.98 };

  const aria = cleanText(element.getAttribute("aria-label"));
  if (aria) return { text: aria, reference: "aria-label", score: 0.96 };

  const own = ownActionText(element);
  if (own) return { text: own, reference: "content", score: 0.94 };

  const placeholder = cleanText(element.getAttribute("placeholder"));
  if (placeholder) return { text: placeholder, reference: "placeholder", score: 0.8 };

  const title = cleanText(element.getAttribute("title"));
  if (title) return { text: title, reference: "title", score: 0.76 };

  const adjacent = adjacentText(element);
  if (adjacent) return { text: adjacent, reference: "adjacent-text", score: 0.66 };

  const context = contextName(element);
  if (context) return { text: `${context} ${humanize(role)}`, reference: "semantic-context", score: 0.58 };

  return {
    text: `${humanize(role || element.localName)} ${fallbackIndex + 1}`,
    reference: "deterministic-fallback",
    score: 0.35,
  };
}

function humanize(value: string): string {
  const cleaned = cleanText(value.replace(/[-_]+/gu, " "));
  return cleaned ? cleaned[0]!.toUpperCase() + cleaned.slice(1) : "Interaction";
}

function cssEscape(value: string): string {
  if (typeof CSS !== "undefined" && typeof CSS.escape === "function") return CSS.escape(value);
  return value.replace(/[^a-zA-Z0-9_-]/gu, (character) => `\\${character}`);
}

function simpleSelector(element: Element): string {
  const id = cleanText(element.id);
  if (id) return `#${cssEscape(id)}`;
  const parent = element.parentElement;
  const tag = element.localName;
  if (!parent) return tag;
  const siblings = Array.from(parent.children).filter((sibling) => sibling.localName === tag);
  const index = siblings.indexOf(element);
  return index <= 0 && siblings.length === 1 ? tag : `${tag}:nth-of-type(${index + 1})`;
}

function cssFallback(element: Element): string {
  const root = element.getRootNode();
  if (root instanceof Document) {
    return read(simpleSelector(element), () =>
      finder(element, {
        attr: (name) => name === "role" || name === "type",
        className: () => false,
        idName: () => false,
        tagName: () => true,
        timeoutMs: Number.MAX_SAFE_INTEGER,
        seedMinLength: 1,
        optimizedMinLength: 2,
        maxNumberOfPathChecks: 5_000,
      }),
    );
  }
  return simpleSelector(element);
}

function pathNode(element: Element, sourceOrder: number): PathNode {
  const role = semanticRole(element);
  const label = labelFor(element, role || "host", sourceOrder);
  const node: PathNode = { css: cssFallback(element) };
  if (role || label.reference !== "deterministic-fallback") {
    node.semantic = { role: role || "generic", name: label.text };
  }
  return node;
}

function traverse(root: Document | ShadowRoot): ElementRecord[] {
  const records: ElementRecord[] = [];
  let sourceOrder = 0;

  const visit = (currentRoot: Document | ShadowRoot, shadowPath: PathNode[]): void => {
    for (const element of Array.from(currentRoot.querySelectorAll("*"))) {
      records.push({ element, shadowPath, sourceOrder: sourceOrder++ });
      const shadowRoot = read<ShadowRoot | null>(null, () => element.shadowRoot);
      if (shadowRoot?.mode === "open") {
        const hostPath = read<PathNode>(
          { css: read(element.localName, () => simpleSelector(element)) },
          () => pathNode(element, sourceOrder),
        );
        visit(shadowRoot, [...shadowPath, hostPath]);
      }
    }
  };

  visit(root, []);
  return records;
}

function semanticScope(element: Element): SemanticNode[] {
  const reversed: SemanticNode[] = [];
  let current = parentAcrossShadow(element);
  while (current && reversed.length < 4) {
    const role = semanticRole(current);
    if (LANDMARK_ROLES.has(role)) {
      const name = labelFor(current, role, 0);
      if (name.reference !== "deterministic-fallback") {
        reversed.push({ role, name: name.text });
      }
    }
    current = parentAcrossShadow(current);
  }
  return reversed.reverse();
}

function locatorFor(record: ElementRecord, role: string, name: string): Locator {
  return {
    framePath: [],
    shadowPath: record.shadowPath.map((node) => ({
      ...(node.semantic ? { semantic: { ...node.semantic } } : {}),
      ...(node.css ? { css: node.css } : {}),
    })),
    semantic: { scope: semanticScope(record.element), role, name },
    css: cssFallback(record.element),
    evidence: [
      { kind: "accessibility", reference: `semantic:${role}`, score: 0.88 },
      { kind: "dom", reference: `css:${record.element.localName}`, score: 0.65 },
    ],
  };
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
  if (explicitRole(element) === "checkbox" || explicitRole(element) === "radio" ||
      explicitRole(element) === "switch") {
    return "boolean";
  }
  if (explicitRole(element) === "slider" || explicitRole(element) === "spinbutton") {
    return "number";
  }
  return "string";
}

function controlEnum(element: Element): string[] {
  if (!(element instanceof HTMLSelectElement)) return [];
  const seen = new Set<string>();
  const values: string[] = [];
  for (const option of Array.from(element.options)) {
    const text = cleanText(option.label || option.textContent);
    if (text && !seen.has(text)) {
      seen.add(text);
      values.push(text);
    }
  }
  return values;
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
  const result =
    first!.toLocaleLowerCase("en-US") +
    rest.map((word) => word[0]!.toLocaleUpperCase("en-US") + word.slice(1)).join("");
  return /^\p{Number}/u.test(result) ? `value${result}` : result.slice(0, 80);
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

function isNativeControl(element: Element): boolean {
  return (
    (element.matches("input, select, textarea") &&
      !(element instanceof HTMLInputElement && inputType(element) === "hidden")) ||
    element.hasAttribute("contenteditable")
  );
}

function isControl(element: Element): boolean {
  return isNativeControl(element) || CUSTOM_CONTROL_ROLES.has(explicitRole(element));
}

function isAction(element: Element): boolean {
  if (element.matches("button, a[href], summary")) return true;
  if (element instanceof HTMLInputElement) {
    return ["button", "image", "reset", "submit"].includes(inputType(element));
  }
  return ACTION_ROLES.has(explicitRole(element));
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

function controlInteraction(record: ElementRecord): Interaction {
  const role = semanticRole(record.element) || "control";
  const label = labelFor(record.element, role, record.sourceOrder);
  const parameter = parameterFor(record.element, label, record.sourceOrder);
  const description = descriptionFor(record.element);
  return {
    kind: "control",
    framePath: [],
    scope: semanticScope(record.element),
    role,
    name: label.text,
    ...(description ? { description } : {}),
    parameters: [parameter],
    locators: [locatorFor(record, role, label.text)],
    actions: [controlAction(record.element, parameter.name)],
    evidence: evidenceFor(label, record.element),
  };
}

function actionInteraction(record: ElementRecord): Interaction {
  const role = semanticRole(record.element) || "button";
  const label = labelFor(record.element, role, record.sourceOrder);
  const description = descriptionFor(record.element);
  return {
    kind: "action",
    framePath: [],
    scope: semanticScope(record.element),
    role,
    name: label.text,
    ...(description ? { description } : {}),
    parameters: [],
    locators: [locatorFor(record, role, label.text)],
    actions: [
      {
        kind: "click",
        locatorIndexes: [0],
        sideEffect: actionEffect(record.element),
      },
    ],
    evidence: evidenceFor(label, record.element),
  };
}

function associatedForm(element: Element): HTMLFormElement | null {
  if (
    element instanceof HTMLInputElement ||
    element instanceof HTMLSelectElement ||
    element instanceof HTMLTextAreaElement ||
    element instanceof HTMLButtonElement
  ) {
    return element.form;
  }
  return element.closest("form");
}

function formInteraction(formRecord: ElementRecord, records: ElementRecord[]): Interaction {
  const form = formRecord.element as HTMLFormElement;
  const role = explicitRole(form) || (form.getAttribute("role") === "search" ? "search" : "form");
  const label = labelFor(form, role, formRecord.sourceOrder);
  const members = records.filter(
    (record) =>
      !isHidden(record.element) &&
      (isControl(record.element) || isAction(record.element)) &&
      associatedForm(record.element) === form,
  );
  const controls = members.filter((record) => isControl(record.element));
  const names = new Map<string, number>();
  const parameters: Parameter[] = [];
  const locators: Locator[] = [];
  const actions: Action[] = [];

  for (const record of controls) {
    const controlRole = semanticRole(record.element) || "control";
    const controlLabel = labelFor(record.element, controlRole, record.sourceOrder);
    const base = parameterName(controlLabel.text);
    const occurrence = (names.get(base) ?? 0) + 1;
    names.set(base, occurrence);
    const name = occurrence === 1 ? base : `${base}_${occurrence}`;
    parameters.push(parameterFor(record.element, controlLabel, record.sourceOrder, name));
    locators.push(locatorFor(record, controlRole, controlLabel.text));
    actions.push(controlAction(record.element, name, locators.length - 1));
  }

  for (const record of members.filter((member) => isAction(member.element))) {
    const effect = actionEffect(record.element);
    if (effect.class !== "submission") continue;
    const actionRole = semanticRole(record.element) || "button";
    const actionLabel = labelFor(record.element, actionRole, record.sourceOrder);
    locators.push(locatorFor(record, actionRole, actionLabel.text));
    actions.push({
      kind: "click",
      locatorIndexes: [locators.length - 1],
      sideEffect: effect,
    });
  }

  const formLocator = locatorFor(formRecord, role, label.text);
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
    name: label.text,
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

async function exploreSafely(
  records: ElementRecord[],
  options: NormalizedOptions,
): Promise<ExplorationResult> {
  const result: ExplorationResult = { details: new Set(), focused: new Set() };
  if (!options.safeExplore || options.maxOperations === 0) return result;

  const deadline = performance.now() + options.timeoutMs;
  let operations = 0;
  for (const record of records) {
    if (operations >= options.maxOperations || performance.now() >= deadline) break;
    if (
      record.element instanceof HTMLDetailsElement &&
      !record.element.hasAttribute("disabled") &&
      detailsDepth(record.element) <= options.maxDepth
    ) {
      operations++;
      result.details.add(record.element);
      read(undefined, () => {
        record.element.setAttribute("open", "");
      });
    }
  }
  await Promise.resolve();
  return result;
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
  if (exploration.focused.has(element)) {
    interaction.evidence.push({
      kind: "heuristic",
      reference: "exploration:focused-without-input",
      score: 0.55,
    });
  }
}

function disambiguateInteractions(interactions: Interaction[]): void {
  const occurrences = new Map<string, number>();
  for (const interaction of interactions) {
    const scope = interaction.scope.map((node) => `${node.role}\u0000${node.name ?? ""}`).join("\u0001");
    const key = `${interaction.kind}\u0002${scope}\u0002${interaction.role}\u0002${interaction.name.toLocaleLowerCase("en-US")}`;
    const occurrence = (occurrences.get(key) ?? 0) + 1;
    occurrences.set(key, occurrence);
    if (occurrence > 1) {
      interaction.name = `${interaction.name} (${occurrence})`;
      for (const locator of interaction.locators) {
        if (locator.semantic) locator.semantic.name = interaction.name;
      }
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
  let records = traverse(document);
  const exploration = await exploreSafely(records, normalized);
  if (exploration.details.size > 0) records = traverse(document);

  const interactions: Interaction[] = [];
  for (const record of records) {
    if (isHidden(record.element)) continue;
    try {
      if (record.element instanceof HTMLFormElement) {
        interactions.push(formInteraction(record, records));
      }
      if (isControl(record.element)) {
        const interaction = controlInteraction(record);
        explorationEvidence(interaction, record.element, exploration);
        interactions.push(interaction);
      } else if (isAction(record.element)) {
        const interaction = actionInteraction(record);
        explorationEvidence(interaction, record.element, exploration);
        interactions.push(interaction);
      }
    } catch {
      // Element-level failures are intentionally isolated so the remaining
      // deterministic observations are still returned.
    }
  }
  disambiguateInteractions(interactions);

  return {
    coverageReported: false,
    frames: [],
    interactions,
  };
}

globalThis.__GEOVISOR_EXTRACT__ = extract;

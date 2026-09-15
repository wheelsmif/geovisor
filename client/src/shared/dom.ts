// DOM access helpers shared by the extractor and the apply runtime.
//
// Shared code must be realm-agnostic. The extractor only ever sees elements
// from the document it was injected into, but the runtime resolves across frame
// boundaries, and an element inside an iframe is an instance of *that* frame's
// HTMLInputElement, not the top document's. So these helpers identify elements
// structurally -- localName, node type, presence of a property -- and never
// with `instanceof` against a DOM interface.

import { cleanText, DESCRIPTION_LIMIT } from "./text";

const TEXT_NODE = 3;
const DOCUMENT_FRAGMENT_NODE = 11;

/** Elements that can be named by a <label> element. */
const LABELABLE = new Set(["input", "select", "textarea"]);

type ReadFailureHook = () => void;

let readFailureHook: ReadFailureHook | undefined;

/**
 * Counts `read` failures for the duration of `fn`, then restores the previous
 * hook. Finder timeouts and other expected degradations should use
 * `readExpected` so they are not counted as coverage gaps.
 */
export function beginReadAccounting(): () => number {
  let failures = 0;
  let done = false;
  const previous = readFailureHook;
  readFailureHook = () => {
    failures += 1;
  };
  return () => {
    if (!done) {
      done = true;
      readFailureHook = previous;
    }
    return failures;
  };
}

/** Swallows DOM access failures so one hostile element cannot end a traversal. */
export function read<T>(fallback: T, operation: () => T): T {
  try {
    return operation();
  } catch {
    readFailureHook?.();
    return fallback;
  }
}

/** Like `read`, but the failure is an expected fallback, not a coverage gap. */
export function readExpected<T>(fallback: T, operation: () => T): T {
  try {
    return operation();
  } catch {
    return fallback;
  }
}

export function explicitRole(element: Element): string {
  return cleanText(element.getAttribute("role")).toLowerCase().split(" ")[0] ?? "";
}

export function inputType(element: Element): string {
  return cleanText(element.getAttribute("type") ?? "text").toLowerCase() || "text";
}

export function isTextNode(node: Node): boolean {
  return node.nodeType === TEXT_NODE;
}

export function isLabelable(element: Element): boolean {
  return LABELABLE.has(element.localName);
}

function asShadowRoot(node: Node): ShadowRoot | null {
  return node.nodeType === DOCUMENT_FRAGMENT_NODE && "host" in node
    ? (node as ShadowRoot)
    : null;
}

/** Walks to the parent element, crossing an open shadow boundary at the host. */
export function parentAcrossShadow(element: Element): Element | null {
  if (element.parentElement) return element.parentElement;
  const root = asShadowRoot(element.getRootNode());
  return root ? root.host : null;
}

/** The containing Document or ShadowRoot, when it supports ID lookup. */
function lookupRoot(element: Element): Document | ShadowRoot | null {
  const root = element.getRootNode() as Document | ShadowRoot;
  return typeof root.getElementById === "function" ? root : null;
}

/**
 * Reports whether the element is an editable host.
 *
 * `hasAttribute("contenteditable")` is true for the string "false", so the
 * attribute value is what decides.
 */
export function isContentEditable(element: Element): boolean {
  const value = element.getAttribute("contenteditable");
  if (value === null) return false;
  const normalized = value.trim().toLowerCase();
  return normalized === "" || normalized === "true" || normalized === "plaintext-only";
}

/**
 * Reports whether an element carries a current value that ACCNAME must skip.
 *
 * Accessible-name computation does not include the contents of embedded
 * controls. `textarea.textContent` is the current value, so treating it as
 * name text leaked drafts and PII.
 */
function isValueBearingControl(element: Element): boolean {
  const tag = element.localName;
  if (tag === "textarea" || tag === "select") return true;
  if (tag === "input") return inputType(element) !== "hidden";
  return isContentEditable(element);
}

/**
 * Concatenates descendant text while skipping embedded controls and `skip`.
 *
 * This is the ACCNAME "name from contents" walk used for labels and
 * aria-labelledby / aria-describedby targets.
 */
export function subtreeNameText(element: Element | null, skip: Element | null = null): string {
  if (!element) return "";
  return cleanText(collectNameText(element, skip));
}

function collectNameText(node: Node, skip: Element | null): string {
  if (skip && node === skip) return "";
  if (isTextNode(node)) return node.textContent ?? "";
  if (node.nodeType !== 1) return "";
  const element = node as Element;
  if (element !== skip && isValueBearingControl(element)) return "";
  let text = "";
  for (const child of Array.from(element.childNodes)) {
    const piece = collectNameText(child, skip);
    if (piece) text += ` ${piece}`;
  }
  return text;
}

/** Resolves an IDREF list attribute to the concatenated text of its targets. */
export function referencedText(
  element: Element,
  attribute: string,
  limit = DESCRIPTION_LIMIT,
): string {
  const ids = cleanText(element.getAttribute(attribute), limit).split(" ").filter(Boolean);
  const root = lookupRoot(element);
  if (!root) return "";
  return cleanText(
    ids
      .map((id) => read("", () => subtreeNameText(root.getElementById(id), element)))
      .filter(Boolean)
      .join(" "),
    limit,
  );
}

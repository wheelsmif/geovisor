// DOM access helpers shared by the extractor and the generated WebMCP runtime.
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

/** Swallows DOM access failures so one hostile element cannot end a traversal. */
export function read<T>(fallback: T, operation: () => T): T {
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
      .map((id) => read("", () => cleanText(root.getElementById(id)?.textContent)))
      .filter(Boolean)
      .join(" "),
  );
}

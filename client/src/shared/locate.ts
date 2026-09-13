// The single element-addressing implementation.
//
// Resolution runs in the browser at tool-execution time, but it lives here
// beside the role and name functions the extractor uses to *record* locators,
// so producer and consumer cannot drift.

import { read } from "./dom";
import { accessibleName } from "./name";
import { normalizeRole, semanticRole } from "./role";

export interface SemanticNode {
  role: string;
  name?: string;
  /**
   * Index into the ordered set of elements this node matches; 0 when absent.
   *
   * This is how ambiguity among elements with the same role and name is
   * resolved (GV-004). It exists because the alternative -- mutating the name
   * to make it unique -- produces a name no element in the DOM carries, so the
   * locator can never match.
   */
  nth?: number;
}

export interface SemanticLocator extends SemanticNode {
  scope?: SemanticNode[];
}

export interface PathNode {
  semantic?: SemanticNode;
  cssFallback?: string;
}

export interface LocatorCandidate {
  id: string;
  framePath: PathNode[];
  shadowPath: PathNode[];
  semantic?: SemanticLocator;
  cssFallback?: string;
}

type Root = Document | ShadowRoot | Element;

/** The name a recorded locator name is compared against. */
function matchName(element: Element): string {
  return accessibleName(element, semanticRole(element))?.text ?? "";
}

function matchRole(element: Element): string {
  return normalizeRole(semanticRole(element));
}

/** The role a frame path node addresses. */
export const FRAME_ROLE = "iframe";

function candidates(root: Root): Element[] {
  if (!root || typeof root.querySelectorAll !== "function") {
    throw new Error("locator root does not support DOM queries");
  }
  return Array.from(root.querySelectorAll("*"));
}

/**
 * The frames of one document, in the order the browser source numbers them.
 *
 * A frame is addressed by its index among the containing document's frames
 * (GV-003), and that index cannot be expressed as a CSS selector: `:nth-child`
 * counts among element siblings, so two single-frame wrappers both answer to
 * index 0. It also cannot use the ordinary element scan, which does not enter
 * shadow roots, because a frame hosted in a shadow root is still one of its
 * document's frames and is counted as such upstream.
 *
 * This mirrors `collectFrameOwnerOrder` in internal/browser/frames.go: pre-order
 * descent, light children before open-shadow content, localName iframe|frame
 * (not computed role), and no descent past a frame, whose contents belong to a
 * different document. Closed shadows are invisible to `element.shadowRoot`.
 */
export function frameCandidates(root: Root): Element[] {
  const frames: Element[] = [];
  const visit = (element: Element): void => {
    const tag = element.localName;
    if (tag === "iframe" || tag === "frame") {
      frames.push(element);
      return;
    }
    for (const child of Array.from(element.children)) visit(child);
    const shadow = read<ShadowRoot | null>(null, () => element.shadowRoot);
    if (shadow) {
      for (const child of Array.from(shadow.children)) visit(child);
    }
  };
  for (const child of Array.from(root.children)) visit(child);
  return frames;
}

/**
 * Finds the elements in `elements` matching a semantic node.
 *
 * A single pass computes role and name once per element (GV-035); the previous
 * runtime rescanned the subtree and recomputed accessible names for every
 * element once per scope node per resolution attempt. Scope hops still scan
 * the *new* root — the matched ancestor — because that is a different tree.
 * Per-action memoization is rejected: apply mutates the DOM between tools.
 */
function matchIn(elements: Element[], node: SemanticNode): Element[] {
  const wantRole = normalizeRole(node.role);
  const wantName = Object.prototype.hasOwnProperty.call(node, "name") ? node.name : undefined;
  const matches: Element[] = [];
  for (const element of elements) {
    if (matchRole(element) !== wantRole) continue;
    if (wantName !== undefined && matchName(element) !== wantName) continue;
    matches.push(element);
  }
  return matches;
}

function matchAll(root: Root, node: SemanticNode): Element[] {
  return matchIn(candidates(root), node);
}

function describe(node: SemanticNode): string {
  const name = Object.prototype.hasOwnProperty.call(node, "name")
    ? ` and name ${JSON.stringify(node.name)}`
    : "";
  return `role ${JSON.stringify(normalizeRole(node.role))}${name}`;
}

function resolveSemanticNode(root: Root, node: SemanticNode): Element {
  const matches = matchAll(root, node);
  const index = node.nth ?? 0;
  const match = matches[index];
  if (!match) {
    throw new Error(
      matches.length === 0
        ? `no element with ${describe(node)}`
        : `${describe(node)} matched ${matches.length} element(s), so there is no index ${index}`,
    );
  }
  return match;
}

function resolveSemantic(root: Root, semantic: SemanticLocator): Element {
  let scope: Root = root;
  for (const node of semantic.scope ?? []) {
    scope = resolveSemanticNode(scope, node);
  }
  return resolveSemanticNode(scope, semantic);
}

/**
 * The ordered elements a semantic locator matches under `root`, or none when its
 * scope chain does not resolve.
 *
 * Exported so the extractor can check a locator against the very matcher the
 * runtime will use before recording it: a locator the producer cannot resolve is
 * one the consumer cannot resolve either. Sharing the matcher is what makes that
 * check meaningful rather than a second opinion.
 */
export function semanticMatches(root: Root, semantic: SemanticLocator): Element[] {
  let scope: Root = root;
  for (const node of semantic.scope ?? []) {
    const found = matchAll(scope, node)[node.nth ?? 0];
    if (!found) return [];
    scope = found;
  }
  return matchAll(scope, semantic);
}

/** Attempts a CSS strategy, recording why it did not resolve. */
function tryCSS(root: Root, selector: string, failures: string[]): Element | null {
  try {
    const match = root.querySelector(selector);
    if (match) return match;
    failures.push(`CSS ${JSON.stringify(selector)} matched no element`);
  } catch (error) {
    failures.push(`CSS ${JSON.stringify(selector)} is invalid: ${messageOf(error)}`);
  }
  return null;
}

function resolvePathNode(root: Root, node: PathNode): Element {
  const failures: string[] = [];
  if (node.semantic) {
    try {
      return resolveSemanticNode(root, node.semantic);
    } catch (error) {
      failures.push(`semantic: ${messageOf(error)}`);
    }
  }
  if (node.cssFallback) {
    const match = tryCSS(root, node.cssFallback, failures);
    if (match) return match;
  }
  throw new Error(failures.join("; ") || "path node has no locator strategy");
}

/**
 * Resolves one step of a frame path.
 *
 * Frames get their own resolver because they are addressed differently: by
 * position among the containing document's frames rather than by a match within
 * an element scan. Routing them through the ordinary path-node resolver is what
 * made GV-003 silent -- an unmatched semantic half fell through to a CSS
 * fallback that counted the wrong thing and returned the wrong frame.
 */
function resolveFrameNode(root: Root, node: PathNode): Element {
  const failures: string[] = [];
  if (node.semantic) {
    const frames = frameCandidates(root);
    const matches = matchIn(frames, node.semantic);
    const index = node.semantic.nth ?? 0;
    const match = matches[index];
    if (match) return match;
    failures.push(
      `semantic: ${describe(node.semantic)} matched ${matches.length} of ` +
        `${frames.length} frame(s) in this document, so there is no index ${index}`,
    );
  }
  if (node.cssFallback) {
    const match = tryCSS(root, node.cssFallback, failures);
    if (match) return match;
  }
  throw new Error(failures.join("; ") || "frame path node has no locator strategy");
}

export function resolveCandidate(candidate: LocatorCandidate): Element {
  let root: Root = document;
  for (let index = 0; index < candidate.framePath.length; index += 1) {
    const frame = resolveFrameNode(root, candidate.framePath[index]!);
    if (matchRole(frame) !== FRAME_ROLE) {
      throw new Error(`framePath[${index}] resolved to <${frame.localName}>, not a frame`);
    }
    let childDocument: Document | null;
    try {
      childDocument = (frame as HTMLIFrameElement).contentDocument;
    } catch (error) {
      throw new Error(`framePath[${index}] is cross-origin and inaccessible: ${messageOf(error)}`);
    }
    if (!childDocument) {
      throw new Error(
        `framePath[${index}] is cross-origin, unavailable, or not loaded; ` +
          "page JavaScript cannot bypass this browser boundary",
      );
    }
    root = childDocument;
  }
  for (let index = 0; index < candidate.shadowPath.length; index += 1) {
    const host = resolvePathNode(root, candidate.shadowPath[index]!);
    if (!host.shadowRoot) {
      throw new Error(`shadowPath[${index}] resolved to a host without an open shadow root`);
    }
    root = host.shadowRoot;
  }

  const failures: string[] = [];
  if (candidate.semantic) {
    try {
      return resolveSemantic(root, candidate.semantic);
    } catch (error) {
      failures.push(`semantic: ${messageOf(error)}`);
    }
  }
  if (candidate.cssFallback) {
    const match = tryCSS(root, candidate.cssFallback, failures);
    if (match) return match;
  }
  throw new Error(failures.join("; ") || "candidate has no locator strategy");
}

export function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

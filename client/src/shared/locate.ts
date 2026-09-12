// The single element-addressing implementation.
//
// Resolution runs in the browser at tool-execution time, but it lives here
// beside the role and name functions the extractor uses to *record* locators,
// so producer and consumer cannot drift.

import { accessibleName } from "./name";
import { normalizeRole, semanticRole } from "./role";

export interface SemanticNode {
  role: string;
  name?: string;
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

function candidates(root: Root): Element[] {
  if (!root || typeof root.querySelectorAll !== "function") {
    throw new Error("locator root does not support DOM queries");
  }
  return Array.from(root.querySelectorAll("*"));
}

/**
 * Finds the elements under `root` matching a semantic node.
 *
 * A single scan computes role and name once per element (GV-035); the previous
 * runtime rescanned the subtree and recomputed accessible names for every
 * element once per scope node per resolution attempt.
 */
function matchAll(root: Root, node: SemanticNode): Element[] {
  const wantRole = normalizeRole(node.role);
  const wantName = Object.prototype.hasOwnProperty.call(node, "name") ? node.name : undefined;
  const matches: Element[] = [];
  for (const element of candidates(root)) {
    if (matchRole(element) !== wantRole) continue;
    if (wantName !== undefined && matchName(element) !== wantName) continue;
    matches.push(element);
  }
  return matches;
}

function describe(node: SemanticNode): string {
  const name = Object.prototype.hasOwnProperty.call(node, "name")
    ? ` and name ${JSON.stringify(node.name)}`
    : "";
  return `role ${JSON.stringify(normalizeRole(node.role))}${name}`;
}

function resolveSemanticNode(root: Root, node: SemanticNode): Element {
  const matches = matchAll(root, node);
  const first = matches[0];
  if (!first) throw new Error(`no element with ${describe(node)}`);
  return first;
}

function resolveSemantic(root: Root, semantic: SemanticLocator): Element {
  let scope: Root = root;
  for (const node of semantic.scope ?? []) {
    scope = resolveSemanticNode(scope, node);
  }
  return resolveSemanticNode(scope, semantic);
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
    try {
      const match = root.querySelector(node.cssFallback);
      if (match) return match;
      failures.push(`CSS ${JSON.stringify(node.cssFallback)} matched no element`);
    } catch (error) {
      failures.push(`CSS ${JSON.stringify(node.cssFallback)} is invalid: ${messageOf(error)}`);
    }
  }
  throw new Error(failures.join("; ") || "path node has no locator strategy");
}

export function resolveCandidate(candidate: LocatorCandidate): Element {
  let root: Root = document;
  for (let index = 0; index < candidate.framePath.length; index += 1) {
    const frame = resolvePathNode(root, candidate.framePath[index]!);
    if (frame.localName !== "iframe" && frame.localName !== "frame") {
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
    try {
      const match = root.querySelector(candidate.cssFallback);
      if (match) return match;
      failures.push(`CSS ${JSON.stringify(candidate.cssFallback)} matched no element`);
    } catch (error) {
      failures.push(`CSS ${JSON.stringify(candidate.cssFallback)} is invalid: ${messageOf(error)}`);
    }
  }
  throw new Error(failures.join("; ") || "candidate has no locator strategy");
}

export function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

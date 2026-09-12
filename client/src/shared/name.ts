// The single accessible-name implementation.
//
// The extractor records a name in each locator and the generated runtime
// recomputes it at execution time to find the element again. Those were two
// different algorithms with different source precedence, so names recorded by
// one were not always findable by the other.
//
// Every source here is recomputable from the element alone, which is what makes
// a recorded name a usable match key. The extractor's positional fallback
// ("Textbox 3") is deliberately *not* part of this function: it depends on a
// document-wide traversal counter the runtime cannot know, so it is a
// parameter-name hint only and never a match key or tool identity (GV-018).

import { cleanText, humanize } from "./text";
import {
  inputType,
  isLabelable,
  isTextNode,
  parentAcrossShadow,
  read,
  referencedText,
} from "./dom";

export type NameSource =
  | "label"
  | "aria-labelledby"
  | "aria-label"
  | "content"
  | "placeholder"
  | "title"
  | "adjacent-text"
  | "semantic-context";

export interface NameResult {
  text: string;
  source: NameSource;
  score: number;
}

const ADJACENT_TEXT_LIMIT = 120;

/**
 * Computes the accessible name of an element, or null when no name can be
 * derived from the element and its surroundings.
 */
export function accessibleName(element: Element, role: string): NameResult | null {
  const label = labelsText(element);
  if (label) return { text: label, source: "label", score: 0.98 };

  const labelled = referencedText(element, "aria-labelledby");
  if (labelled) return { text: labelled, source: "aria-labelledby", score: 0.98 };

  const aria = cleanText(element.getAttribute("aria-label"));
  if (aria) return { text: aria, source: "aria-label", score: 0.96 };

  const own = ownActionText(element);
  if (own) return { text: own, source: "content", score: 0.94 };

  const placeholder = cleanText(element.getAttribute("placeholder"));
  if (placeholder) return { text: placeholder, source: "placeholder", score: 0.8 };

  const title = cleanText(element.getAttribute("title"));
  if (title) return { text: title, source: "title", score: 0.76 };

  const adjacent = adjacentText(element);
  if (adjacent) return { text: adjacent, source: "adjacent-text", score: 0.66 };

  const context = contextName(element);
  if (context) {
    return { text: `${context} ${humanize(role)}`, source: "semantic-context", score: 0.58 };
  }

  return null;
}

function labelsText(element: Element): string {
  if (!isLabelable(element)) return "";
  const labels = (element as HTMLInputElement).labels;
  return cleanText(
    Array.from(labels ?? [])
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
  if (element.localName === "input" && inputType(element) === "image") {
    return cleanText(element.getAttribute("alt"));
  }
  return "";
}

function adjacentText(element: Element): string {
  const before = element.previousElementSibling;
  if (before) {
    const text = cleanText(before.textContent);
    if (text && text.length <= ADJACENT_TEXT_LIMIT) return text;
  }
  const parent = element.parentElement;
  if (!parent) return "";
  const direct = Array.from(parent.childNodes)
    .filter(isTextNode)
    .map((node) => cleanText(node.textContent))
    .filter(Boolean)
    .join(" ");
  return direct.length <= ADJACENT_TEXT_LIMIT ? cleanText(direct) : "";
}

function contextName(element: Element): string {
  let current = parentAcrossShadow(element);
  while (current) {
    const labelled = referencedText(current, "aria-labelledby");
    const aria = cleanText(current.getAttribute("aria-label"));
    const legend =
      current.localName === "fieldset"
        ? cleanText(read("", () => current!.querySelector(":scope > legend")?.textContent ?? ""))
        : "";
    const heading = cleanText(
      read("", () => current!.querySelector(":scope > h1, :scope > h2, :scope > h3")?.textContent ?? ""),
    );
    const name = labelled || aria || legend || heading;
    if (name) return name;
    current = parentAcrossShadow(current);
  }
  return "";
}

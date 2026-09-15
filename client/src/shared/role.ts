// The single role-resolution implementation (GV-049).
//
// This used to exist twice: once here for the extractor and once as a copy
// in the apply path. The runtime copy knew fewer elements
// than the extractor, so locators scoped by <details>, <fieldset>, <nav>,
// <dialog>, <summary>, or a heading could never match and degraded silently to
// the CSS fallback. Producer and consumer now resolve roles through this
// function, which makes that class of divergence impossible rather than merely
// tested.

import { explicitRole, inputType, isContentEditable } from "./dom";

export { isContentEditable };

/**
 * The role used when an element has no mapped role. Locators record "generic"
 * rather than an empty string, so both sides normalize through this constant
 * before comparing.
 */
export const GENERIC_ROLE = "generic";

export function semanticRole(element: Element): string {
  const explicit = explicitRole(element);
  if (explicit) return explicit;

  const tag = element.localName;
  if (tag === "form") return "form";
  if (tag === "nav") return "navigation";
  if (tag === "button" || tag === "summary") return "button";
  if (tag === "a" && element.hasAttribute("href")) return "link";
  if (tag === "textarea" || isContentEditable(element)) return "textbox";
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
  // HTML-AAM gives <iframe> no corresponding ARIA role, but a frame path node
  // has to name what it addresses, and a role checked against this table is
  // better than a role that is merely asserted. See FRAME_ROLE in locate.ts.
  if (tag === "iframe" || tag === "frame") return "iframe";
  return "";
}

/** Normalizes an absent role so producer and consumer compare equal. */
export function normalizeRole(role: string): string {
  return role || GENERIC_ROLE;
}

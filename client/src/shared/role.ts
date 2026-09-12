// The single role-resolution implementation (GV-049).
//
// This used to exist twice: once here for the extractor and once as a Go string
// literal in internal/emitter/webmcp.go. The runtime copy knew fewer elements
// than the extractor, so locators scoped by <details>, <fieldset>, <nav>,
// <dialog>, <summary>, or a heading could never match and degraded silently to
// the CSS fallback. Producer and consumer now resolve roles through this
// function, which makes that class of divergence impossible rather than merely
// tested.

import { explicitRole, inputType } from "./dom";

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
  return "";
}

/**
 * Reports whether the element is an editable host.
 *
 * `hasAttribute("contenteditable")` is true for the string "false" (GV-005), so
 * the attribute value is what decides.
 */
export function isContentEditable(element: Element): boolean {
  const value = element.getAttribute("contenteditable");
  if (value === null) return false;
  const normalized = value.trim().toLowerCase();
  return normalized === "" || normalized === "true" || normalized === "plaintext-only";
}

/** Normalizes an absent role so producer and consumer compare equal. */
export function normalizeRole(role: string): string {
  return role || GENERIC_ROLE;
}

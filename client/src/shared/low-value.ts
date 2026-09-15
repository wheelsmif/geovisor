// Low-value action predicates shared with the Go compiler.
// Pattern strings live in internal/compiler/lowvalue_patterns.json so the
// extractor and compiler cannot drift on citation, DOI, RFC, or #cite rules.
// This module is realm-agnostic: it only accepts strings.

import patterns from "../../../internal/compiler/lowvalue_patterns.json";

const CITATION_MARK = new RegExp(patterns.citationMarkName, "u");
const DOI_NAME = new RegExp(patterns.doiName, "iu");
const BARE_RFC_OR_DOI = new RegExp(patterns.bareRFCOrDOI, "iu");
const CITATION_FRAGMENT = new RegExp(patterns.citationFragment, "iu");

/** Citation marks, DOI/RFC tokens, and unnamed links. Unnamed buttons are kept. */
export function isLowValueName(name: string, role: string): boolean {
  if (role === "link" && name === "") {
    return true;
  }
  return CITATION_MARK.test(name) || DOI_NAME.test(name) || BARE_RFC_OR_DOI.test(name);
}

/** Citation fragment in a live href hash or a locator CSS string. */
export function isCitationFragment(text: string): boolean {
  return CITATION_FRAGMENT.test(text);
}
